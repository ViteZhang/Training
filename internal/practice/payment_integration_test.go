package practice_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/pay"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/membership"
	"peetraining-server/internal/params"
	"peetraining-server/internal/payment"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/store"
)

// 验收：兑换会员后，批改、出题、解析额度立即不限（解析按会员每月 1000 页，PRD 13.1）。
func TestRedeemLiftsQuota(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, _ := f.user(t)
	qs := quota.New(dbq.New(f.db), params.New(dbq.New(f.db)), func() time.Time { return f.clock })
	consume := func(typ quota.Type, key string) error {
		return store.WithTx(ctx, f.db, func(q *dbq.Queries) error {
			return qs.Consume(ctx, q, quota.Charge{UserID: uid, Type: typ, Amount: 1, Key: key})
		})
	}
	for i := range 3 {
		if err := consume(quota.Grading, "g"+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := consume(quota.Grading, "g-4"); kind(err) != apperr.QuotaExceeded {
		t.Fatalf("免费版每天 3 次批改：%v", err)
	}
	seedCode(t, f, "MBRS2345", "monthly", 0, f.clock.Add(24*time.Hour), "active", "unused")
	if _, err := membership.New(membership.Deps{DB: f.db, Now: func() time.Time { return f.clock }}).Redeem(ctx, uid, "MBRS2345"); err != nil {
		t.Fatal(err)
	}
	for _, typ := range []quota.Type{quota.Grading, quota.AIQuestions, quota.PaperGrading, quota.EssayGrading, quota.ImportQuestions} {
		if left, err := qs.Remaining(ctx, uid, typ); err != nil || left != nil {
			t.Errorf("%s 兑换后应不限：%v %v", typ, left, err)
		}
	}
	if left, _ := qs.Remaining(ctx, uid, quota.ParsePages); left == nil || *left != 1000 {
		t.Errorf("会员解析每月 1000 页：%v", left)
	}
	if err := consume(quota.Grading, "g-4"); err != nil {
		t.Errorf("兑换后立即可以继续批改：%v", err)
	}
}

func paymentFx(t *testing.T, f *fx) (*payment.Service, *pay.Mock) {
	t.Helper()
	q := dbq.New(f.db)
	ps := params.New(q)
	m := pay.NewMock("test")
	return payment.New(payment.Deps{DB: f.db, Params: ps, Quota: quota.New(q, ps, func() time.Time { return f.clock }), Flags: flags.New(q), Gateway: m,
		NotifyBaseURL: "https://x/api/v1", AppleBundleID: "cn.dreamelab.training", Log: logx.New(io.Discard, slog.LevelInfo), Now: func() time.Time { return f.clock }}), m
}

func notify(m *pay.Mock, orderNo, txn string, cents int64) (http.Header, []byte) {
	body := payment.MockBody(orderNo, txn, cents)
	h := http.Header{}
	h.Set("X-Mock-Signature", m.Sign(body))
	return h, body
}

func countMemberships(t *testing.T, f *fx, uid uint64, source string) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM memberships WHERE owner_user_id = ? AND source = ? AND revoked_at IS NULL", uid, source).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// 验收：支付用 mock 走通下单 → 回调 → 开通；重复回调不重复开通。
func TestMockPaymentFlow(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, _ := f.user(t)
	other, _ := f.user(t)
	svc, _ := paymentFx(t, f)

	// 开关默认关闭：会员中心只显示兑换码入口，下单 404。
	c, err := svc.Center(ctx, uid)
	if err != nil || c.PaymentEnabled || len(c.Plans) != 3 || len(c.Benefits) != 6 {
		t.Fatalf("会员中心：%+v %v", c, err)
	}
	if c.Plans[0].Tier != "sprint" || !c.Plans[0].Available || c.Plans[1].Available || c.Plans[1].UnavailableReason == "" || !c.Plans[1].Recommended ||
		c.Plans[2].PriceCents != 2990 || !c.Plans[2].EndsAt.Equal(f.clock.AddDate(0, 0, 30)) {
		t.Errorf("档位：冲刺卡可买、考季卡缺次年初试日期、月卡 30 天：%+v", c.Plans)
	}
	if _, err := svc.CreateOrder(ctx, uid, "monthly", pay.ChannelWechat, ""); kind(err) != apperr.NotFound {
		t.Fatalf("开关关闭时下单 404：%v", err)
	}
	if _, err := f.db.Exec("UPDATE feature_flags SET enabled_for_all = 1 WHERE flag_key = 'online_payment'"); err != nil {
		t.Fatal(err)
	}
	svc, m := paymentFx(t, f)
	if c, _ := svc.Center(ctx, uid); !c.PaymentEnabled || len(c.Channels) != 3 {
		t.Errorf("开关打开后可以支付：%+v", c)
	}
	if _, err := svc.CreateOrder(ctx, uid, "season", pay.ChannelWechat, ""); kind(err) != apperr.Conflict {
		t.Errorf("考季卡缺次年初试日期不能买：%v", err)
	}
	if _, err := svc.CreateOrder(ctx, uid, "gift", pay.ChannelWechat, ""); kind(err) != apperr.BadRequest {
		t.Errorf("不存在的档位：%v", err)
	}

	// 下单：同一幂等键返回同一订单。
	o, err := svc.CreateOrder(ctx, uid, "monthly", pay.ChannelWechat, "idem-key-1")
	if err != nil || o.Status != "created" || o.AmountCents != 2990 || o.Prepay["mock"] != "1" {
		t.Fatalf("下单：%+v %v", o, err)
	}
	if o2, err := svc.CreateOrder(ctx, uid, "monthly", pay.ChannelWechat, "idem-key-1"); err != nil || o2.OrderNo != o.OrderNo {
		t.Errorf("幂等：%+v %v", o2, err)
	}
	if _, err := svc.GetOrder(ctx, other, o.OrderNo); kind(err) != apperr.NotFound {
		t.Errorf("别人的订单 404：%v", err)
	}

	// 伪造签名、金额不符都不开通。
	h, body := notify(pay.NewMock("attacker"), o.OrderNo, "TX1", 2990)
	if code, _, _ := svc.HandleNotify(ctx, pay.ChannelWechat, body, h); code == http.StatusOK {
		t.Error("伪造签名的回调应失败")
	}
	h, body = notify(m, o.OrderNo, "TX1", 1)
	if code, _, _ := svc.HandleNotify(ctx, pay.ChannelWechat, body, h); code == http.StatusOK {
		t.Error("金额不符应失败")
	}
	if g, _ := svc.GetOrder(ctx, uid, o.OrderNo); g.Status != "created" || countMemberships(t, f, uid, "order") != 0 {
		t.Fatalf("没开通：%+v", g)
	}

	// 回调 → 开通；重复回调不重复开通。
	h, body = notify(m, o.OrderNo, "TX1", 2990)
	for range 3 {
		if code, _, b := svc.HandleNotify(ctx, pay.ChannelWechat, body, h); code != http.StatusOK || string(b) != `{"code":"SUCCESS","message":"成功"}` {
			t.Fatalf("回调应答：%d %s", code, b)
		}
	}
	g, err := svc.GetOrder(ctx, uid, o.OrderNo)
	if err != nil || g.Status != "paid" || g.PaidAt == nil || g.MembershipEndsAt == nil || !g.MembershipEndsAt.Equal(f.clock.AddDate(0, 0, 30)) {
		t.Fatalf("开通：%+v %v", g, err)
	}
	if n := countMemberships(t, f, uid, "order"); n != 1 {
		t.Errorf("重复回调只开通一次：%d", n)
	}
	if ok, _ := membership.IsMember(ctx, dbq.New(f.db), uid, f.clock); !ok {
		t.Error("开通后是会员")
	}
	var msgs int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM messages WHERE owner_user_id = ? AND mtype = 'membership'", uid).Scan(&msgs)
	if msgs != 1 {
		t.Errorf("开通消息一条：%d", msgs)
	}
	// 支付宝渠道的订单收到微信回调：不开通。
	ali, _ := svc.CreateOrder(ctx, uid, "monthly", pay.ChannelAlipay, "")
	h, body = notify(m, ali.OrderNo, "TX2", 2990)
	if code, _, _ := svc.HandleNotify(ctx, pay.ChannelWechat, body, h); code == http.StatusOK {
		t.Error("渠道不一致应失败")
	}
	// 第二单叠加到第一单之后。
	if code, _, b := svc.HandleNotify(ctx, pay.ChannelAlipay, body, h); code != http.StatusOK || string(b) != "success" {
		t.Fatalf("支付宝应答：%d %s", code, b)
	}
	if g, _ := svc.GetOrder(ctx, uid, ali.OrderNo); g.MembershipEndsAt == nil || !g.MembershipEndsAt.Equal(f.clock.AddDate(0, 0, 60)) {
		t.Errorf("时长叠加：%+v", g)
	}

	// 退款：收回本单会员，第二单不受影响。
	if err := svc.Refund(ctx, o.OrderNo, "功能无法使用", 0); err != nil {
		t.Fatal(err)
	}
	if err := svc.Refund(ctx, o.OrderNo, "重复", 0); kind(err) != apperr.Conflict {
		t.Errorf("已退款不能再退：%v", err)
	}
	if g, _ := svc.GetOrder(ctx, uid, o.OrderNo); g.Status != "refunded" || countMemberships(t, f, uid, "order") != 1 {
		t.Errorf("退款后会员立即失效：%+v", g)
	}
	var refunds int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM refunds WHERE refund_no = ?", "R"+o.OrderNo).Scan(&refunds)
	if refunds != 1 {
		t.Errorf("退款单：%d", refunds)
	}
}

func TestAppleVerify(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, _ := f.user(t)
	other, _ := f.user(t)
	if _, err := f.db.Exec("UPDATE feature_flags SET enabled_for_all = 1 WHERE flag_key = 'online_payment'"); err != nil {
		t.Fatal(err)
	}
	svc, _ := paymentFx(t, f)
	o, err := svc.CreateOrder(ctx, uid, "sprint", pay.ChannelApple, "")
	if err != nil || len(o.AppAccountToken) != 36 || o.AppleProductID != "cn.dreamelab.training.sprint" {
		t.Fatalf("内购下单返回商品与 appAccountToken：%+v %v", o, err)
	}
	cases := map[string]string{
		"cn.dreamelab.training.monthly|" + o.AppAccountToken + "|1":           "product_mismatch",
		"cn.dreamelab.training.sprint|00000000-0000-4000-8000-000000000000|1": "token_mismatch",
		"bad": "transaction_not_found",
	}
	for txn, want := range cases {
		if _, err := svc.AppleVerify(ctx, uid, o.OrderNo, txn); reason(err) != want {
			t.Errorf("%s 应提示 %s：%v", txn, want, err)
		}
	}
	txn := "cn.dreamelab.training.sprint|" + o.AppAccountToken + "|1"
	if _, err := svc.AppleVerify(ctx, other, o.OrderNo, txn); kind(err) != apperr.NotFound {
		t.Errorf("别人的订单 404：%v", err)
	}
	for range 2 {
		g, err := svc.AppleVerify(ctx, uid, o.OrderNo, txn)
		if err != nil || g.Status != "paid" || g.MembershipEndsAt == nil || !g.MembershipEndsAt.Equal(time.Date(2026, 12, 20, 16, 0, 0, 0, time.UTC)) {
			t.Fatalf("内购开通至当年初试结束：%+v %v", g, err)
		}
	}
	if n := countMemberships(t, f, uid, "order"); n != 1 {
		t.Errorf("重复校验只开通一次：%d", n)
	}
}

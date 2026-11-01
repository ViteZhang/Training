package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"peetraining-server/internal/admin"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/logx"
)

// PRD v3 3.4、6.5a、7.4、7.8、13.3（10 月 26 日更新）：购买入口按平台开关；客服收款后单个发码并记账。
func TestIntegrationPurchaseEntryAndManualCode(t *testing.T) {
	cfg := testConfig(t)
	ctx := context.Background()
	log := logx.New(io.Discard, slog.LevelDebug)
	if err := Migrate(ctx, cfg, log, "up"); err != nil {
		t.Fatal(err)
	}
	b, err := Open(ctx, cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.Redis.FlushDB(ctx)
	handler, err := b.Handler()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	f := &adminFx{t: t, b: b, srv: srv}

	if _, err := b.Admin.CreateAdmin(ctx, "root3", "管理员", "13700000019", "Init-pass-2026", []admin.Role{admin.RoleAdmin}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Admin.CreateAdmin(ctx, "kefu3", "客服", "13700000018", "Init-pass-2026", []admin.Role{admin.RoleSupport}, false); err != nil {
		t.Fatal(err)
	}
	root := f.login("root3", "13700000019", "Init-pass-2026")
	support := f.login("kefu3", "13700000018", "Init-pass-2026")

	entry := func(platform string) gen.PurchaseEntry {
		code, body := f.do("GET", "/api/v1/purchase-entry?platform="+platform, "", "")
		if code != 200 {
			t.Fatalf("购买入口：%d %s", code, body)
		}
		var e gen.PurchaseEntry
		_ = json.Unmarshal([]byte(body), &e)
		return e
	}
	// 默认：安卓开、iOS 关；关闭时不返回客服信息。
	if e := entry("android"); !e.Enabled || e.Contact == nil || e.Contact.Note == "" {
		t.Errorf("安卓默认显示购买入口：%+v", e)
	}
	if e := entry("ios"); e.Enabled || e.Contact != nil {
		t.Errorf("iOS 默认关闭且不带客服信息：%+v", e)
	}

	// 7.8：客服不能改；没填客服信息不能打开；填好后 iOS 打开立即生效。
	if code, _ := f.do("GET", "/api/v1/admin/config/purchase-entry", support, ""); code != 403 {
		t.Errorf("客服不能看购买入口配置：%d", code)
	}
	_, body := f.do("GET", "/api/v1/admin/config/purchase-entry", root, "")
	var pe gen.AdminPurchaseEntry
	_ = json.Unmarshal([]byte(body), &pe)
	if !pe.Android || pe.Ios || pe.Wecom.QrUploaded {
		t.Fatalf("初始配置：%+v", pe)
	}
	in := map[string]any{"android": true, "ios": true, "version": pe.Version,
		"wecom": map[string]any{"name": "", "wecom_id": "", "reply_time": "工作日", "note": "加客服"}}
	raw, _ := json.Marshal(in)
	if code, _ := f.do("PUT", "/api/v1/admin/config/purchase-entry", root, string(raw)); code != 400 {
		t.Errorf("没填客服名称和 ID 不能打开：%d", code)
	}
	in["wecom"] = map[string]any{"name": "考研Training 客服", "wecom_id": "kaoyan-training", "reply_time": "工作日 9:00–21:00", "note": "加客服获取兑换码"}
	raw, _ = json.Marshal(in)
	if code, body := f.do("PUT", "/api/v1/admin/config/purchase-entry", root, string(raw)); code != 200 {
		t.Fatalf("改购买入口：%d %s", code, body)
	}
	if e := entry("ios"); !e.Enabled || e.Contact == nil || e.Contact.WecomId != "kaoyan-training" || e.Contact.QrUrl != nil {
		t.Errorf("iOS 打开后立即生效：%+v", e)
	}
	if code, _ := f.do("PUT", "/api/v1/admin/config/purchase-entry", root, string(raw)); code != 409 {
		t.Errorf("旧版本号再改应冲突：%d", code)
	}

	// 上传二维码：只收 PNG / JPEG，内容要与声明一致。
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	qr := `{"content_type":"image/png","data_base64":"` + base64.StdEncoding.EncodeToString(png) + `"}`
	if code, body := f.do("PUT", "/api/v1/admin/config/purchase-entry/qr", root, qr); code != 200 || !strings.Contains(body, `"qr_uploaded":true`) {
		t.Fatalf("上传二维码：%d %s", code, body)
	}
	bad := `{"content_type":"image/jpeg","data_base64":"` + base64.StdEncoding.EncodeToString(png) + `"}`
	if code, _ := f.do("PUT", "/api/v1/admin/config/purchase-entry/qr", root, bad); code != 400 {
		t.Errorf("声明 JPEG 实为 PNG 应拒绝：%d", code)
	}
	if e := entry("ios"); e.Contact == nil || e.Contact.QrUrl == nil {
		t.Errorf("上传后 App 拿到二维码地址：%+v", e)
	}
	// 企业微信 ID 选填：有二维码时可以不填 ID，App 拿到空 ID 就只显示二维码。
	_, body = f.do("GET", "/api/v1/admin/config/purchase-entry", root, "")
	_ = json.Unmarshal([]byte(body), &pe)
	noID, _ := json.Marshal(map[string]any{"android": true, "ios": true, "version": pe.Version,
		"wecom": map[string]any{"name": "考研Training 客服", "wecom_id": "", "reply_time": "每天 9:00–22:00", "note": ""}})
	if code, body := f.do("PUT", "/api/v1/admin/config/purchase-entry", root, string(noID)); code != 200 {
		t.Fatalf("有二维码时 ID 可以不填：%d %s", code, body)
	}
	if e := entry("android"); e.Contact == nil || e.Contact.WecomId != "" || e.Contact.QrUrl == nil {
		t.Errorf("没填 ID：%+v", e)
	}

	// 7.4 单个发码：客服可以发；收入记入 7.3（渠道企业微信客服）；码不绑定手机号，兑换后订单补上兑换人。
	issue := `{"phone":"13900001234","tier":"monthly","amount_cents":2990,"pay_method":"企业微信转账","remark":"小红书来的"}`
	code, body := f.do("POST", "/api/v1/admin/redeem/single-codes", support, issue)
	if code != 200 {
		t.Fatalf("单个发码：%d %s", code, body)
	}
	var issued struct {
		Code    string `json:"code"`
		OrderNo string `json:"order_no"`
		BatchID int64  `json:"batch_id"`
	}
	_ = json.Unmarshal([]byte(body), &issued)
	if len(issued.Code) != 8 || !strings.HasPrefix(issued.OrderNo, "W") {
		t.Fatalf("单个发码结果：%+v", issued)
	}
	code, body = f.do("POST", "/api/v1/admin/redeem/single-codes", support, issue)
	var second struct {
		BatchID int64 `json:"batch_id"`
	}
	_ = json.Unmarshal([]byte(body), &second)
	if code != 200 || second.BatchID != issued.BatchID {
		t.Errorf("同档位的单个发码归入同一批次：%d %s", code, body)
	}
	if code, _ := f.do("POST", "/api/v1/admin/redeem/single-codes", support, `{"phone":"123","tier":"monthly","amount_cents":2990,"pay_method":"x"}`); code != 400 {
		t.Errorf("手机号不对应拒绝：%d", code)
	}
	_, body = f.do("GET", "/api/v1/admin/orders/summary", root, "")
	var sum gen.AdminOrderSummary
	_ = json.Unmarshal([]byte(body), &sum)
	if sum.MonthRevenueCents != 2*2990 {
		t.Errorf("手动收款计入本月收入：%+v", sum)
	}
	_, body = f.do("GET", "/api/v1/admin/orders", root, "")
	if !strings.Contains(body, `"channel":"wecom"`) || !strings.Contains(body, "139****1234") || strings.Contains(body, "13900001234") {
		t.Errorf("订单列表显示渠道与脱敏手机号：%s", body)
	}
	_, body = f.do("GET", "/api/v1/admin/redeem/batches", root, "")
	if !strings.Contains(body, "企业微信客服 · 单个发码") {
		t.Errorf("批次列表里有单个发码批次：%s", body)
	}

	// 另一个手机号的用户也能兑换（码不绑定手机号）；兑换后订单补上兑换人，退款时收回会员。
	res, err := b.DB.Exec("INSERT INTO users (phone, invite_code, nickname) VALUES ('13900005678', 'INVT5678', '小王')")
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := res.LastInsertId()
	if _, err := b.Membership.Redeem(ctx, uint64(uid), strings.ToLower(issued.Code)); err != nil {
		t.Fatalf("兑换：%v", err)
	}
	var owner int64
	_ = b.DB.QueryRow("SELECT IFNULL(owner_user_id, 0) FROM orders WHERE order_no = ?", issued.OrderNo).Scan(&owner)
	if owner != uid {
		t.Errorf("兑换后订单补上兑换人：%d", owner)
	}
	if code, body := f.do("POST", "/api/v1/admin/orders/"+issued.OrderNo+"/refund", root, `{"reason":"用户误购"}`); code != 204 && code != 200 {
		t.Fatalf("退款：%d %s", code, body)
	}
	var revoked int
	_ = b.DB.QueryRow("SELECT COUNT(*) FROM memberships WHERE owner_user_id = ? AND revoked_at IS NOT NULL", uid).Scan(&revoked)
	if revoked != 1 {
		t.Errorf("退款收回会员：%d", revoked)
	}

	// 还没兑换的码退款：码作废，之后不能再兑换。
	_, body = f.do("POST", "/api/v1/admin/redeem/single-codes", support, issue)
	var third struct {
		Code    string `json:"code"`
		OrderNo string `json:"order_no"`
	}
	_ = json.Unmarshal([]byte(body), &third)
	if code, body := f.do("POST", "/api/v1/admin/orders/"+third.OrderNo+"/refund", root, `{"reason":"客服发错档位"}`); code != 204 && code != 200 {
		t.Fatalf("退款：%d %s", code, body)
	}
	if _, err := b.Membership.Redeem(ctx, uint64(uid), third.Code); err == nil {
		t.Error("退款后的码不能再兑换")
	}
}

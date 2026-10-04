package practice_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"peetraining-server/internal/dbq"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/invite"
	"peetraining-server/internal/params"
	"peetraining-server/internal/store"
)

func inviteDays(t *testing.T, f *fx, uid uint64) int {
	t.Helper()
	var d int
	if err := f.db.QueryRow("SELECT COALESCE(SUM(TIMESTAMPDIFF(DAY, starts_at, ends_at)), 0) FROM memberships WHERE owner_user_id = ? AND source = 'invite' AND revoked_at IS NULL", uid).Scan(&d); err != nil {
		t.Fatal(err)
	}
	return d
}

// 验收：被邀请人只注册不导入时不发奖励；导入后双方会员正确延长；邀请人累计上限 70 天。
func TestInviteReward(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	q := dbq.New(f.db)
	newSvc := func() *invite.Service {
		return invite.New(f.db, params.New(q), flags.New(q), func() time.Time { return f.clock })
	}
	svc := newSvc()
	f.imp.OnConfirmedTx = func(ctx context.Context, q *dbq.Queries, userID uint64) error { return svc.Activate(ctx, q, userID) }
	inviter, _ := f.user(t)
	var code string
	_ = f.db.QueryRow("SELECT invite_code FROM users WHERE id = ?", inviter).Scan(&code)
	bind := func(invitee uint64, c string) {
		t.Helper()
		if err := store.WithTx(ctx, f.db, func(q *dbq.Queries) error { return svc.Bind(ctx, q, invitee, c, f.clock) }); err != nil {
			t.Fatal(err)
		}
	}

	// 开关关闭时不绑定。
	early, esid := f.user(t)
	bind(early, code)
	f.imported(t, early, esid)
	if inviteDays(t, f, early) != 0 || inviteDays(t, f, inviter) != 0 {
		t.Fatal("开关关闭时不发奖励")
	}
	if _, err := f.db.Exec("UPDATE feature_flags SET enabled_for_all = 1 WHERE flag_key = 'invite'"); err != nil {
		t.Fatal(err)
	}
	svc = newSvc()

	friend, fsid := f.user(t)
	bind(friend, " "+strings.ToLower(code)+" ")
	bind(inviter, code) // 自己的码忽略
	bind(friend, "NOPE2345")
	if inviteDays(t, f, friend) != 0 || inviteDays(t, f, inviter) != 0 {
		t.Fatal("只注册不导入时不发奖励")
	}
	o, err := svc.Overview(ctx, inviter)
	if err != nil || o.Code != code || o.Invited != 1 || o.EarnedDays != 0 || o.Records[0].Activated || o.RewardDays != 7 || o.MaxDays != 70 {
		t.Fatalf("邀请记录：%+v %v", o, err)
	}
	f.imported(t, friend, fsid) // 两次确认导入，只发一次
	if inviteDays(t, f, friend) != 7 || inviteDays(t, f, inviter) != 7 {
		t.Fatalf("导入后双方各 7 天：%d %d", inviteDays(t, f, friend), inviteDays(t, f, inviter))
	}
	if o, _ := svc.Overview(ctx, inviter); o.EarnedDays != 7 || !o.Records[0].Activated || o.Records[0].Days != 7 {
		t.Errorf("已获得天数：%+v", o)
	}

	// 上限：再邀 10 个，邀请人累计 70 天，第 11 个好友照常得 7 天。
	for i := range 10 {
		u, sid := f.user(t)
		bind(u, code)
		f.imported(t, u, sid)
		if i == 9 && inviteDays(t, f, u) != 7 {
			t.Errorf("超过上限后好友仍得 7 天：%d", inviteDays(t, f, u))
		}
	}
	if d := inviteDays(t, f, inviter); d != 70 {
		t.Errorf("邀请人累计上限 70 天：%d", d)
	}
	if o, _ := svc.Overview(ctx, inviter); o.EarnedDays != 70 || o.Invited != 11 || o.Records[0].Days != 0 {
		t.Errorf("达到上限后的记录：%+v", o.Records[0])
	}
	if o, _ := svc.Overview(ctx, friend); o.Invited != 0 {
		t.Error("别人的邀请记录互不可见")
	}
}

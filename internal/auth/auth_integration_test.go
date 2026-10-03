package auth_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/auth"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/cloud/sms"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/params"
	"peetraining-server/internal/store"
	"peetraining-server/internal/testenv"
)

type fixture struct {
	svc   *auth.Service
	db    *sql.DB
	rdb   *redis.Client
	sms   *sms.Mock
	oss   *oss.Mock
	flags *flags.Service
	clock *time.Time
}

func setup(t *testing.T) *fixture {
	t.Helper()
	env := testenv.New(t)
	ctx := context.Background()
	db, err := store.OpenMySQL(ctx, env.MySQLDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	log := logx.New(io.Discard, slog.LevelInfo)
	if err := store.MigrateUp(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	rdb, err := store.OpenRedis(ctx, store.RedisOptions(env.RedisAddr, "", env.RedisDB))
	if err != nil {
		t.Fatal(err)
	}
	rdb.FlushDB(ctx)
	t.Cleanup(func() { rdb.FlushDB(context.Background()); rdb.Close() })

	now := time.Date(2026, 10, 2, 2, 0, 0, 0, time.UTC)
	f := &fixture{db: db, rdb: rdb, sms: sms.NewMock(), oss: oss.NewMock(), clock: &now}
	f.svc = auth.New(auth.Deps{
		DB: db, Redis: rdb, SMS: f.sms, OSS: f.oss, Params: params.New(dbq.New(db)),
		JWTSecret: "test-secret-test-secret-test-secret", Logger: log,
		Now: func() time.Time { return *f.clock },
	})
	f.flags = flags.New(dbq.New(db))
	return f
}

var dev = auth.Device{ID: "device-0001", Name: "iPhone 15", Platform: "ios"}

const phone = "13812345678"

func (f *fixture) login(t *testing.T, p string, d auth.Device) auth.LoginResult {
	t.Helper()
	ctx := context.Background()
	f.rdb.Del(ctx, "sms:cd:"+p) // 跳过 60 秒冷却
	if _, err := f.svc.SendLoginCode(ctx, p, true, "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	code, _ := f.sms.LastCode(p)
	res, err := f.svc.Login(ctx, p, code, d, "")
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func kindOf(err error) apperr.Kind {
	if e, ok := apperr.As(err); ok {
		return e.Kind
	}
	return 0
}

func TestLoginCreatesUserAndIssuesTokens(t *testing.T) {
	f := setup(t)
	res := f.login(t, phone, dev)
	if !res.IsNew || res.User.Phone != phone || len(res.User.InviteCode) != 8 || res.User.OnboardingStep != "1.1" {
		t.Fatalf("新用户：%+v", res)
	}
	uid, did, err := f.svc.ParseAccess(res.Tokens.AccessToken)
	if err != nil || uid != res.User.ID || did != dev.ID {
		t.Fatalf("访问令牌：%d %s %v", uid, did, err)
	}
	// 老用户再次登录。
	again := f.login(t, phone, dev)
	if again.IsNew || again.User.ID != res.User.ID {
		t.Fatal("老用户不应重复创建")
	}
	// 同一设备重新登录后，旧刷新令牌作废。
	if _, err := f.svc.Refresh(context.Background(), res.Tokens.RefreshToken, dev.ID); kindOf(err) != apperr.Unauthorized {
		t.Fatalf("旧刷新令牌应失效：%v", err)
	}
}

func TestSmsRules(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.svc.SendLoginCode(ctx, phone, false, ""); kindOf(err) != apperr.BadRequest {
		t.Fatal("未同意协议不发码（0.2b）")
	}
	r, err := f.svc.SendLoginCode(ctx, phone, true, "")
	if err != nil || r.ResendAfterSeconds != 60 || r.RemainingToday != 9 {
		t.Fatalf("首次发送：%+v %v", r, err)
	}
	_, err = f.svc.SendLoginCode(ctx, phone, true, "")
	if e, _ := apperr.As(err); e == nil || e.Kind != apperr.TooManyRequests || e.Detail["reason"] != "cooldown" {
		t.Fatalf("60 秒内不能重发：%v", err)
	}
	// 每天最多 10 次。
	for i := 2; i <= 10; i++ {
		f.rdb.Del(ctx, "sms:cd:"+phone)
		if _, err := f.svc.SendLoginCode(ctx, phone, true, ""); err != nil {
			t.Fatalf("第 %d 次：%v", i, err)
		}
	}
	f.rdb.Del(ctx, "sms:cd:"+phone)
	_, err = f.svc.SendLoginCode(ctx, phone, true, "")
	if e, _ := apperr.As(err); e == nil || e.Detail["reason"] != "daily_limit" {
		t.Fatalf("第 11 次应超出每日上限：%v", err)
	}
}

func TestWrongCodeAttempts(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	_, _ = f.svc.SendLoginCode(ctx, phone, true, "")
	code, _ := f.sms.LastCode(phone)
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 1; i <= 4; i++ {
		_, err := f.svc.Login(ctx, phone, wrong, dev, "")
		e, _ := apperr.As(err)
		if e == nil || e.Detail["remaining_attempts"] != 5-i {
			t.Fatalf("第 %d 次错误：%v %v", i, err, e)
		}
	}
	_, err := f.svc.Login(ctx, phone, wrong, dev, "")
	if e, _ := apperr.As(err); e == nil || e.Detail["code_expired"] != true {
		t.Fatalf("错 5 次失效：%v", err)
	}
	if _, err := f.svc.Login(ctx, phone, code, dev, ""); kindOf(err) != apperr.BadRequest {
		t.Fatal("失效后正确的码也不能用")
	}
	// 验证码 5 分钟有效：用新码但把 Redis 里的码删掉模拟过期。
	f.rdb.Del(ctx, "sms:cd:"+phone)
	_, _ = f.svc.SendLoginCode(ctx, phone, true, "")
	code, _ = f.sms.LastCode(phone)
	f.rdb.Del(ctx, "sms:code:login:"+phone)
	if _, err := f.svc.Login(ctx, phone, code, dev, ""); kindOf(err) != apperr.BadRequest {
		t.Fatal("过期的码不能用")
	}
}

func TestRefreshRotationAndReuse(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	res := f.login(t, phone, dev)
	next, err := f.svc.Refresh(ctx, res.Tokens.RefreshToken, dev.ID)
	if err != nil || next.RefreshToken == res.Tokens.RefreshToken {
		t.Fatalf("刷新：%v", err)
	}
	if _, err := f.svc.Refresh(ctx, next.RefreshToken, "other-device"); kindOf(err) != apperr.Unauthorized {
		t.Fatal("设备不符应 401")
	}
	// 旧令牌被复用：作废该设备全部令牌，连新令牌也不能用。
	if _, err := f.svc.Refresh(ctx, res.Tokens.RefreshToken, dev.ID); kindOf(err) != apperr.Unauthorized {
		t.Fatal("旧令牌复用应 401")
	}
	if _, err := f.svc.Refresh(ctx, next.RefreshToken, dev.ID); kindOf(err) != apperr.Unauthorized {
		t.Fatal("复用后该设备的新令牌也应作废")
	}
	// 过期。
	again := f.login(t, phone, dev)
	*f.clock = f.clock.Add(61 * 24 * time.Hour)
	if _, err := f.svc.Refresh(ctx, again.Tokens.RefreshToken, dev.ID); kindOf(err) != apperr.Unauthorized {
		t.Fatal("过期的刷新令牌应 401")
	}
	if _, err := f.svc.Refresh(ctx, "nope", dev.ID); kindOf(err) != apperr.Unauthorized {
		t.Fatal("不存在的令牌应 401")
	}
}

func TestDevices(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	res := f.login(t, phone, dev)
	other := auth.Device{ID: "device-0002", Name: "小米 14", Platform: "android"}
	f.login(t, phone, other)
	list, err := f.svc.Devices(ctx, res.User.ID, dev.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("两台设备：%+v %v", list, err)
	}
	if err := f.svc.RemoveDevice(ctx, res.User.ID, dev.ID, dev.ID); kindOf(err) != apperr.BadRequest {
		t.Fatal("不能移除本机")
	}
	if err := f.svc.RemoveDevice(ctx, res.User.ID, other.ID, dev.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RemoveDevice(ctx, res.User.ID, other.ID, dev.ID); kindOf(err) != apperr.NotFound {
		t.Fatal("已移除的设备再移除返回 404")
	}
	// 别人的设备：404。
	stranger := f.login(t, "13900000000", auth.Device{ID: "device-x", Platform: "ios"})
	if err := f.svc.RemoveDevice(ctx, stranger.User.ID, dev.ID, "device-x"); kindOf(err) != apperr.NotFound {
		t.Fatal("移除别人的设备应 404")
	}
	if err := f.svc.Logout(ctx, res.User.ID, dev.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := f.svc.Devices(ctx, res.User.ID, dev.ID); len(list) != 0 {
		t.Fatalf("退出后设备列表为空：%+v", list)
	}
}

func TestDeletionCoolingAndPurge(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	res := f.login(t, phone, dev)
	f.oss.Seed("u/"+itoa(res.User.ID)+"/materials/a.pdf", []byte("x"))
	f.oss.Seed("u/999/materials/b.pdf", []byte("y"))

	due, err := f.svc.RequestDeletion(ctx, res.User.ID)
	if err != nil || !due.Equal(f.clock.Add(7*24*time.Hour)) {
		t.Fatalf("冷静期：%v %v", due, err)
	}
	if _, err := f.svc.Refresh(ctx, res.Tokens.RefreshToken, dev.ID); kindOf(err) != apperr.Unauthorized {
		t.Fatal("申请注销后退出全部设备")
	}
	// 冷静期内登录即撤销。
	back := f.login(t, phone, dev)
	if !back.DeletionCanceled || back.User.Status != dbq.UsersStatusActive {
		t.Fatalf("登录撤销注销：%+v", back)
	}
	// 再次申请，7 天后到期删除：数据库与 OSS 里该用户内容全部删除，别人的不动。
	if _, err := f.svc.RequestDeletion(ctx, res.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "INSERT INTO subjects (owner_user_id, name) VALUES (?, '中国文学')", res.User.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.svc.PurgeDue(ctx, 10); n != 0 {
		t.Fatal("冷静期未到不删除")
	}
	*f.clock = f.clock.Add(7*24*time.Hour + time.Minute)
	if n, err := f.svc.PurgeDue(ctx, 10); err != nil || n != 1 {
		t.Fatalf("到期删除：%d %v", n, err)
	}
	var cnt int
	_ = f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE id = ?", res.User.ID).Scan(&cnt)
	var subj int
	_ = f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM subjects WHERE owner_user_id = ?", res.User.ID).Scan(&subj)
	if cnt != 0 || subj != 0 {
		t.Fatalf("数据库里还有该用户的数据：users=%d subjects=%d", cnt, subj)
	}
	if keys := f.oss.Keys(); len(keys) != 1 || keys[0] != "u/999/materials/b.pdf" {
		t.Fatalf("OSS：%v", keys)
	}
}

func TestChangePhone(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	res := f.login(t, phone, dev)
	other := f.login(t, "13900000000", auth.Device{ID: "device-x", Platform: "ios"})
	newPhone := "15000000000"

	if _, err := f.svc.SendChangePhoneCode(ctx, res.User.ID, "13700000000", auth.PurposeChangePhoneOld, ""); kindOf(err) != apperr.BadRequest {
		t.Fatal("旧号码必须是当前号码")
	}
	if _, err := f.svc.SendChangePhoneCode(ctx, res.User.ID, other.User.Phone, auth.PurposeChangePhoneNew, ""); kindOf(err) != apperr.Conflict {
		t.Fatal("新号码已注册应 409")
	}
	f.rdb.Del(ctx, "sms:cd:"+phone)
	if _, err := f.svc.SendChangePhoneCode(ctx, res.User.ID, phone, auth.PurposeChangePhoneOld, ""); err != nil {
		t.Fatal(err)
	}
	oldCode, _ := f.sms.LastCode(phone)
	if _, err := f.svc.SendChangePhoneCode(ctx, res.User.ID, newPhone, auth.PurposeChangePhoneNew, ""); err != nil {
		t.Fatal(err)
	}
	newCode, _ := f.sms.LastCode(newPhone)
	// 登录验证码不能用于换号。
	if _, err := f.svc.ChangePhone(ctx, res.User.ID, newCode, newPhone, oldCode); kindOf(err) != apperr.BadRequest {
		t.Fatal("新旧验证码不能互换")
	}
	f.rdb.Del(ctx, "sms:cd:"+phone, "sms:cd:"+newPhone)
	_, _ = f.svc.SendChangePhoneCode(ctx, res.User.ID, phone, auth.PurposeChangePhoneOld, "")
	oldCode, _ = f.sms.LastCode(phone)
	_, _ = f.svc.SendChangePhoneCode(ctx, res.User.ID, newPhone, auth.PurposeChangePhoneNew, "")
	newCode, _ = f.sms.LastCode(newPhone)
	u, err := f.svc.ChangePhone(ctx, res.User.ID, oldCode, newPhone, newCode)
	if err != nil || u.Phone != newPhone {
		t.Fatalf("换号：%+v %v", u, err)
	}
}

func TestBootstrapAndAgreements(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		if _, err := f.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	past := f.clock.Add(-24 * time.Hour)
	exec("INSERT INTO agreements (kind, version, title, body, effective_at, published_at) VALUES ('user','1.0','用户协议','正文',?,?),('privacy','1.0','隐私政策','正文',?,?)", past, past, past, past)
	exec("INSERT INTO app_versions (platform, latest_version, min_version, download_url) VALUES ('android','1.2.0','1.1.0','https://x/apk')")

	// 未登录：只有全局开关与版本信息。
	b, err := f.svc.GetBootstrap(ctx, f.flags, 0, "android", "1.0.9")
	if err != nil || b.LoggedIn || b.Update == nil || !b.Update.Force || !b.Update.HasUpdate || b.Flags["online_payment"] {
		t.Fatalf("未登录：%+v %v", b, err)
	}
	if b, _ := f.svc.GetBootstrap(ctx, f.flags, 0, "android", "1.1.5"); b.Update.Force || !b.Update.HasUpdate {
		t.Fatal("高于最低版本只提示、不强制")
	}
	if b, _ := f.svc.GetBootstrap(ctx, f.flags, 0, "ios", "1.0.0"); b.Update != nil {
		t.Fatal("没配置的平台不返回更新")
	}

	// 登录时同意了当前协议；发布新版后需要重新同意（0.4b）。
	res := f.login(t, phone, dev)
	b, _ = f.svc.GetBootstrap(ctx, f.flags, res.User.ID, "android", "1.2.0")
	if !b.LoggedIn || len(b.Agreements) != 0 || b.OnboardingStep != "1.1" {
		t.Fatalf("刚登录：%+v", b)
	}
	now := *f.clock
	exec("INSERT INTO agreements (kind, version, title, body, change_summary, effective_at, published_at) VALUES ('privacy','1.1','隐私政策','新正文','新增 AI 说明',?,?)", now, now)
	b, _ = f.svc.GetBootstrap(ctx, f.flags, res.User.ID, "android", "1.2.0")
	if len(b.Agreements) != 1 || b.Agreements[0].Version != "1.1" {
		t.Fatalf("新版协议：%+v", b.Agreements)
	}
	if err := f.svc.AcceptAgreements(ctx, res.User.ID, []int64{int64(b.Agreements[0].ID)}); err != nil {
		t.Fatal(err)
	}
	if b, _ = f.svc.GetBootstrap(ctx, f.flags, res.User.ID, "android", "1.2.0"); len(b.Agreements) != 0 {
		t.Fatal("同意后不再弹出")
	}
	if err := f.svc.AcceptAgreements(ctx, res.User.ID, []int64{99999}); kindOf(err) != apperr.NotFound {
		t.Fatal("不存在的协议应 404")
	}
	a, err := f.svc.GetAgreement(ctx, "privacy")
	if err != nil || a.Version != "1.1" {
		t.Fatalf("当前隐私政策：%+v %v", a, err)
	}
	if _, err := f.svc.GetAgreement(ctx, "membership"); kindOf(err) != apperr.NotFound {
		t.Fatal("没发布的协议应 404")
	}

	// 指定用户的功能开关。
	exec("INSERT INTO feature_flag_users (flag_key, user_id) VALUES ('invite', ?)", res.User.ID)
	b, _ = f.svc.GetBootstrap(ctx, f.flags, res.User.ID, "android", "1.2.0")
	if !b.Flags["invite"] || b.Flags["online_payment"] {
		t.Fatalf("指定用户开关：%v", b.Flags)
	}
	b, _ = f.svc.GetBootstrap(ctx, f.flags, 0, "android", "1.2.0")
	if b.Flags["invite"] {
		t.Fatal("其他人看不到")
	}

	// 引导进度。
	step := "1.3"
	me, err := f.svc.UpdateMe(ctx, res.User.ID, nil, &step)
	if err != nil || me.User.OnboardingStep != "1.3" {
		t.Fatalf("引导进度：%v", err)
	}
	bad := "9.9"
	if _, err := f.svc.UpdateMe(ctx, res.User.ID, nil, &bad); kindOf(err) != apperr.BadRequest {
		t.Fatal("非法步骤")
	}
}

func TestBannedUserCannotLogin(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	res := f.login(t, phone, dev)
	if _, err := f.db.ExecContext(ctx, "UPDATE users SET status='banned' WHERE id=?", res.User.ID); err != nil {
		t.Fatal(err)
	}
	f.rdb.Del(ctx, "sms:cd:"+phone)
	_, _ = f.svc.SendLoginCode(ctx, phone, true, "")
	code, _ := f.sms.LastCode(phone)
	if _, err := f.svc.Login(ctx, phone, code, dev, ""); kindOf(err) != apperr.Forbidden {
		t.Fatalf("封禁用户：%v", err)
	}
	if _, err := f.svc.Refresh(ctx, res.Tokens.RefreshToken, dev.ID); kindOf(err) != apperr.Unauthorized {
		t.Fatal("封禁后刷新令牌失效")
	}
}

func TestMembershipInMe(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	res := f.login(t, phone, dev)
	now := *f.clock
	// 两段叠加：当前段到 10 天后，后一段接着到 40 天后。
	if _, err := f.db.ExecContext(ctx, `INSERT INTO memberships (owner_user_id, tier, source, starts_at, ends_at) VALUES
		(?, 'monthly', 'redeem', ?, ?), (?, 'season', 'redeem', ?, ?)`,
		res.User.ID, now.Add(-20*24*time.Hour), now.Add(10*24*time.Hour),
		res.User.ID, now.Add(10*24*time.Hour), now.Add(40*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	me, err := f.svc.GetMe(ctx, res.User.ID)
	if err != nil || !me.Membership.IsMember || me.Membership.Tier != "monthly" || !me.Membership.EndsAt.Equal(now.Add(40*24*time.Hour)) {
		t.Fatalf("会员：%+v %v", me.Membership, err)
	}
}

func itoa(n uint64) string { return fmtUint(n) }

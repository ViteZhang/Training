package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"peetraining-server/internal/admin"
	"peetraining-server/internal/feedback"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/profile"
	"peetraining-server/internal/rules"
)

// secret 只出现在用户的资料、题目与作答原文里：后台任何接口在没有授权时都不应返回它。
const secret = "绝密内容标记XYZ"

type adminFx struct {
	t   *testing.T
	b   *Base
	srv *httptest.Server
}

func (f *adminFx) do(method, path, token, body string) (int, string) {
	f.t.Helper()
	req, _ := http.NewRequest(method, f.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// login 走两步验证：密码 → 短信验证码 → 会话令牌。
func (f *adminFx) login(username, phone, password string) string {
	f.t.Helper()
	code, body := f.do("POST", "/api/v1/admin/auth/login", "", fmt.Sprintf(`{"username":%q,"password":%q}`, username, password))
	if code != 200 {
		f.t.Fatalf("后台登录：%d %s", code, body)
	}
	var ch gen.AdminChallenge
	_ = json.Unmarshal([]byte(body), &ch)
	sms, _ := f.b.Cloud.SMS.(interface{ LastCode(string) (string, bool) }).LastCode(phone)
	code, body = f.do("POST", "/api/v1/admin/auth/verify", "", fmt.Sprintf(`{"challenge_id":%q,"code":%q}`, ch.ChallengeId, sms))
	if code != 200 {
		f.t.Fatalf("两步验证：%d %s", code, body)
	}
	var s gen.AdminSession
	_ = json.Unmarshal([]byte(body), &s)
	return s.Token
}

func TestIntegrationAdmin(t *testing.T) {
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

	// 用户与内容：资料原文、题目、作答都带 secret。
	res, err := b.DB.Exec("INSERT INTO users (phone, invite_code, nickname) VALUES ('13900007777', 'INVT7777', '小林')")
	if err != nil {
		t.Fatal(err)
	}
	uid64, _ := res.LastInsertId()
	uid := uint64(uid64)
	if _, err := b.Profile.Upsert(ctx, uid, profile.Input{ExamYear: 2027, Stage: rules.Strengthen, DailyMinutes: 45}); err != nil {
		t.Fatal(err)
	}
	sub, err := b.Profile.CreateSubject(ctx, uid, profile.SubjectInput{Name: "中国语言文学基础", Code: ptrTo("654"), FullScore: 150})
	if err != nil {
		t.Fatal(err)
	}
	mat, err := b.Material.CreatePasted(ctx, uid, sub.ID, "reference", secret+"讲义", "第一章 "+secret+" 意境是情景交融的艺术境界。", true)
	if err != nil {
		t.Fatal(err)
	}
	var bankID uint64
	_ = b.DB.QueryRow("SELECT id FROM banks WHERE owner_user_id = ?", uid).Scan(&bankID)
	res, err = b.DB.Exec("INSERT INTO questions (bank_id, owner_user_id, qtype, stem, source, content_hash) VALUES (?, ?, 'term', ?, 'exam', SHA2(?, 256))", bankID, uid, "名词解释："+secret, secret)
	if err != nil {
		t.Fatal(err)
	}
	qid, _ := res.LastInsertId()
	res, err = b.DB.Exec("INSERT INTO attempts (owner_user_id, question_id, answer_mode, answer_text) VALUES (?, ?, 'typed', ?)", uid, qid, "我的作答"+secret)
	if err != nil {
		t.Fatal(err)
	}
	aid, _ := res.LastInsertId()
	res, err = b.DB.Exec("INSERT INTO gradings (owner_user_id, attempt_id, kind, status, score, full_score) VALUES (?, ?, 'subjective', 'done', 4, 10)", uid, aid)
	if err != nil {
		t.Fatal(err)
	}
	gid, _ := res.LastInsertId()
	res, err = b.DB.Exec("INSERT INTO disputes (owner_user_id, grading_id, reason, allow_access, score_before) VALUES (?, ?, 'hit_missed', 1, 4)", uid, gid)
	if err != nil {
		t.Fatal(err)
	}
	did, _ := res.LastInsertId()
	fbSvc := feedback.New(b.DB, nil)
	noGrant, err := fbSvc.Submit(ctx, uid, feedback.Input{Type: "recognition", Content: "第 3 页识别错了", MaterialID: mat.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Admin.Aggregate(ctx); err != nil {
		t.Fatal(err)
	}

	// 后台账号：客服、数据分析、管理员；客服是首次登录须改密码的初始账号。
	mk := func(name, phone string, roles []admin.Role, mustChange bool) {
		if _, err := b.Admin.CreateAdmin(ctx, name, name, phone, "Init-pass-2026", roles, mustChange); err != nil {
			t.Fatal(err)
		}
	}
	mk("support1", "13700000001", []admin.Role{admin.RoleSupport}, true)
	mk("analyst1", "13700000002", []admin.Role{admin.RoleAnalyst}, false)
	mk("root", "13700000003", []admin.Role{admin.RoleAdmin}, false)

	// 两步验证：密码错误、验证码错误都不签发会话。
	if code, _ := f.do("POST", "/api/v1/admin/auth/login", "", `{"username":"support1","password":"wrong-password"}`); code != 401 {
		t.Errorf("密码错误：%d", code)
	}
	code, body := f.do("POST", "/api/v1/admin/auth/login", "", `{"username":"support1","password":"Init-pass-2026"}`)
	var ch gen.AdminChallenge
	_ = json.Unmarshal([]byte(body), &ch)
	if code != 200 || ch.PhoneMasked != "137****0001" {
		t.Fatalf("登录第一步：%d %s", code, body)
	}
	if code, _ := f.do("POST", "/api/v1/admin/auth/verify", "", fmt.Sprintf(`{"challenge_id":%q,"code":"000000"}`, ch.ChallengeId)); code != 400 {
		t.Errorf("验证码错误：%d", code)
	}
	support := f.login("support1", "13700000001", "Init-pass-2026")

	// 首次登录必须改密码，改之前只能看自己的账号。
	if code, _ := f.do("GET", "/api/v1/admin/users", support, ""); code != 403 {
		t.Errorf("没改初始密码时不能用：%d", code)
	}
	if code, body := f.do("POST", "/api/v1/admin/me/password", support, `{"old_password":"Init-pass-2026","new_password":"Support-pass-2026"}`); code != 204 {
		t.Fatalf("改密码：%d %s", code, body)
	}
	support = f.login("support1", "13700000001", "Support-pass-2026")
	analyst := f.login("analyst1", "13700000002", "Init-pass-2026")
	root := f.login("root", "13700000003", "Init-pass-2026")

	// App 的用户令牌拿不到后台接口；后台令牌也不能当用户令牌用。
	if code, body := f.do("POST", "/api/v1/auth/sms-codes", "", `{"phone":"13900007777","purpose":"login","agree":true}`); code != 200 {
		t.Fatalf("用户发码：%d %s", code, body)
	}
	smsCode, _ := b.Cloud.SMS.(interface{ LastCode(string) (string, bool) }).LastCode("13900007777")
	_, body = f.do("POST", "/api/v1/auth/login", "", `{"phone":"13900007777","code":"`+smsCode+`","device":{"device_id":"device-7777","platform":"ios"}}`)
	var login gen.LoginResponse
	_ = json.Unmarshal([]byte(body), &login)
	userTok := login.AccessToken
	if code, _ := f.do("GET", "/api/v1/admin/overview", userTok, ""); code != 401 {
		t.Errorf("用户令牌访问后台：%d", code)
	}
	if code, _ := f.do("GET", "/api/v1/me", root, ""); code != 401 {
		t.Errorf("后台令牌访问 App 接口：%d", code)
	}

	// 角色：数据分析只看概览；客服不能退款、不能看完整手机号、不能建兑换码。
	if code, _ := f.do("GET", "/api/v1/admin/overview", analyst, ""); code != 200 {
		t.Errorf("数据分析看概览：%d", code)
	}
	if code, _ := f.do("GET", "/api/v1/admin/users", analyst, ""); code != 403 {
		t.Errorf("数据分析看用户：%d", code)
	}
	for _, c := range []struct{ m, p, b string }{
		{"GET", "/api/v1/admin/users/" + strconv.FormatUint(uid, 10) + "/phone", ""},
		{"POST", "/api/v1/admin/orders/T1/refund", `{"reason":"测试退款"}`},
		{"POST", "/api/v1/admin/redeem/batches", `{"name":"x","tier":"gift","days":7,"quantity":1,"expires_at":"2030-01-01T00:00:00Z"}`},
	} {
		if code, _ := f.do(c.m, c.p, support, c.b); code != 403 {
			t.Errorf("客服 %s %s：%d", c.m, c.p, code)
		}
	}

	// 验收：客服在没有授权时调用任何后台读接口都拿不到用户资料、题目或作答原文。
	ids := map[string]string{"userId": strconv.FormatUint(uid, 10), "orderNo": "T0", "batchId": "1", "codeId": "1", "jobId": "1",
		"disputeId": strconv.FormatInt(did, 10), "feedbackId": strconv.FormatUint(noGrant.ID, 10)}
	// 先撤销异议的授权：这一轮全部按「没有授权」检查。
	if _, err := b.DB.Exec("UPDATE content_access_grants SET revoked_at = NOW(3) WHERE user_id = ?", uid); err != nil {
		t.Fatal(err)
	}
	spec, _ := gen.GetSwagger()
	checked := 0
	for path, item := range spec.Paths.Map() {
		if !strings.HasPrefix(path, "/admin/") || item.Get == nil {
			continue
		}
		p := path
		for k, v := range ids {
			p = strings.ReplaceAll(p, "{"+k+"}", v)
		}
		if strings.Contains(p, "/redeem/codes") {
			p += "?code=ABCD2345"
		}
		code, body := f.do("GET", "/api/v1"+p, support, "")
		if code >= 500 {
			t.Errorf("GET %s：%d %s", p, code, body)
		}
		if strings.Contains(body, secret) {
			t.Errorf("没有授权时 GET %s 返回了用户内容：%s", p, body)
		}
		checked++
	}
	if checked < 20 {
		t.Errorf("后台读接口检查得太少：%d", checked)
	}
	if code, _ := f.do("GET", "/api/v1/admin/disputes/"+ids["disputeId"]+"/content", support, ""); code != 403 {
		t.Errorf("没有授权看异议内容：%d", code)
	}
	if code, _ := f.do("GET", "/api/v1/admin/feedbacks/"+ids["feedbackId"]+"/material", support, ""); code != 403 {
		t.Errorf("没有授权看反馈资料：%d", code)
	}

	// 验收：授权后 72 小时内可查看，每次查看用户都收到消息，日志可查。
	granted, err := fbSvc.Submit(ctx, uid, feedback.Input{Type: "recognition", Content: "第 1 页识别错了", AllowAccess: true, MaterialID: mat.ID})
	if err != nil {
		t.Fatal(err)
	}
	code, body = f.do("GET", "/api/v1/admin/feedbacks/"+strconv.FormatUint(granted.ID, 10), support, "")
	if code != 200 || strings.Contains(body, secret) || !strings.Contains(body, "grant_expires_at") {
		t.Errorf("反馈详情显示授权截止时间、不带资料内容：%d %s", code, body)
	}
	for i := range 2 {
		code, body = f.do("GET", "/api/v1/admin/feedbacks/"+strconv.FormatUint(granted.ID, 10)+"/material", support, "")
		if code != 200 || !strings.Contains(body, secret) {
			t.Fatalf("授权后查看资料（第 %d 次）：%d %s", i+1, code, body)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var notices int
	_ = b.DB.QueryRow("SELECT COUNT(*) FROM messages WHERE owner_user_id = ? AND mtype = 'content_accessed'", uid).Scan(&notices)
	if notices < 1 {
		t.Errorf("查看后用户收到消息：%d", notices)
	}
	code, body = f.do("GET", "/api/v1/admin/access-logs?user_id="+strconv.FormatUint(uid, 10), support, "")
	var logs struct {
		Items []gen.AdminAccessLog `json:"items"`
	}
	_ = json.Unmarshal([]byte(body), &logs)
	if code != 200 || len(logs.Items) != 2 || logs.Items[0].AdminName != "support1" {
		t.Errorf("每次查看都有日志：%d %s", code, body)
	}
	// 用户在 App 的消息中心看到（T27 接口经完整路由）。
	if code, body := f.do("GET", "/api/v1/messages", userTok, ""); code != 200 || !strings.Contains(body, "content_accessed") {
		t.Errorf("用户消息中心：%d %s", code, body)
	}
	if _, err := b.DB.Exec("UPDATE content_access_grants SET expires_at = DATE_SUB(NOW(3), INTERVAL 1 SECOND) WHERE source = 'feedback' AND source_id = ?", granted.ID); err != nil {
		t.Fatal(err)
	}
	if code, _ := f.do("GET", "/api/v1/admin/feedbacks/"+strconv.FormatUint(granted.ID, 10)+"/material", support, ""); code != 403 {
		t.Errorf("授权过期后不能看：%d", code)
	}

	// 验收：能生成一批兑换码并导出（明文只在生成时返回）；用户兑换后单码查询能看到使用人。
	code, body = f.do("POST", "/api/v1/admin/redeem/batches", root, `{"name":"内测第一批","tier":"gift","days":7,"quantity":20,"expires_at":"2030-01-01T00:00:00Z","channel":"内测群"}`)
	var created gen.AdminBatchCreated
	_ = json.Unmarshal([]byte(body), &created)
	if code != 200 || len(created.Codes) != 20 || created.Batch.Quantity != 20 {
		t.Fatalf("生成兑换码：%d %s", code, body)
	}
	if _, err := b.Membership.Redeem(ctx, uid, created.Codes[0]); err != nil {
		t.Fatal(err)
	}
	code, body = f.do("GET", "/api/v1/admin/redeem/codes?code="+created.Codes[0], support, "")
	var one gen.AdminCode
	_ = json.Unmarshal([]byte(body), &one)
	if code != 200 || one.Status != "used" || one.UsedBy == nil || *one.UsedBy != int64(uid) {
		t.Errorf("单码查询：%d %s", code, body)
	}
	code, body = f.do("GET", fmt.Sprintf("/api/v1/admin/redeem/batches/%d", created.Batch.Id), support, "")
	if code != 200 || strings.Count(body, `"tail"`) != 20 || strings.Contains(body, created.Codes[1]) {
		t.Errorf("批次明细只有末 3 位：%d", code)
	}
	if code, _ := f.do("POST", fmt.Sprintf("/api/v1/admin/redeem/codes/%d/void", one.Id), root, ""); code != 204 {
		t.Errorf("作废单码：%d", code)
	}
	var revoked int
	_ = b.DB.QueryRow("SELECT COUNT(*) FROM memberships WHERE owner_user_id = ? AND source = 'redeem' AND revoked_at IS NOT NULL", uid).Scan(&revoked)
	if revoked != 1 {
		t.Errorf("作废后收回会员：%d", revoked)
	}

	// 用户运营操作与审计。
	if code, _ := f.do("POST", "/api/v1/admin/users/"+ids["userId"]+"/parse-pages", support, `{"pages":50,"idempotency_key":"grant-0001"}`); code != 204 {
		t.Errorf("加解析额度：%d", code)
	}
	if code, _ := f.do("POST", "/api/v1/admin/users/"+ids["userId"]+"/parse-pages", support, `{"pages":50,"idempotency_key":"grant-0001"}`); code != 204 {
		t.Errorf("重复提交：%d", code)
	}
	if left, _ := b.Quota.Remaining(ctx, uid, "parse_pages"); left == nil || *left < 140 {
		t.Errorf("加 50 页后额度：%v", left)
	}
	code, body = f.do("GET", "/api/v1/admin/users/"+ids["userId"], support, "")
	if code != 200 || !strings.Contains(body, "139****7777") || strings.Contains(body, "13900007777") || strings.Contains(body, secret) {
		t.Errorf("用户详情脱敏、无内容：%d %s", code, body)
	}
	if code, body := f.do("GET", "/api/v1/admin/users/"+ids["userId"]+"/phone", root, ""); code != 200 || !strings.Contains(body, "13900007777") {
		t.Errorf("管理员看完整手机号：%d %s", code, body)
	}
	if code, _ := f.do("POST", "/api/v1/admin/users/"+ids["userId"]+"/status", root, `{"banned":true}`); code != 204 {
		t.Errorf("封禁：%d", code)
	}
	if code, _ := f.do("GET", "/api/v1/me", userTok, ""); code == 200 {
		// 访问令牌短时有效，封禁同时作废了刷新令牌；这里只确认账号状态。
		var st string
		_ = b.DB.QueryRow("SELECT status FROM users WHERE id = ?", uid).Scan(&st)
		if st != "banned" {
			t.Errorf("封禁后状态：%s", st)
		}
	}
	code, body = f.do("GET", "/api/v1/admin/audit-logs?target_type=user&target_id="+ids["userId"], root, "")
	var audits struct {
		Items []gen.AdminAudit `json:"items"`
	}
	_ = json.Unmarshal([]byte(body), &audits)
	if code != 200 || len(audits.Items) < 4 {
		t.Errorf("写操作与查看手机号都有操作记录：%d %s", code, body)
	}

	// 概览读聚合表。
	code, body = f.do("GET", "/api/v1/admin/overview", analyst, "")
	var ov gen.AdminOverview
	_ = json.Unmarshal([]byte(body), &ov)
	if code != 200 || ov.UsersTotal < 1 || len(ov.Days) != 7 || len(ov.HotSubjects) == 0 || ov.HotSubjects[0].Name != "654" {
		t.Errorf("概览：%d %s", code, body)
	}

	// 退出后令牌失效。
	if code, _ := f.do("POST", "/api/v1/admin/auth/logout", analyst, ""); code != 204 {
		t.Errorf("退出：%d", code)
	}
	if code, _ := f.do("GET", "/api/v1/admin/overview", analyst, ""); code != 401 {
		t.Errorf("退出后：%d", code)
	}
}

func ptrTo[T any](v T) *T { return &v }

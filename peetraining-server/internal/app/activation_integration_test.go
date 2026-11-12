package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"peetraining-server/internal/admin"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/logx"
)

// 7.20 激活漏斗：各级按业务表推算；北京时间划分注册日期与「隔天」；数据分析只看汇总，客服能看到具体用户。
func TestIntegrationAdminActivation(t *testing.T) {
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

	exec := func(q string, args ...any) uint64 {
		t.Helper()
		res, err := b.DB.Exec(q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		id, _ := res.LastInsertId()
		return uint64(id)
	}
	user := func(phone, createdAt, step string) uint64 {
		return exec("INSERT INTO users (phone, invite_code, onboarding_step, created_at) VALUES (?, ?, ?, ?)", phone, "I"+phone[5:], step, createdAt)
	}
	// 北京时间 10 月 1 日注册的三个人（第一个是北京时间 0:30，UTC 还在 9 月 30 日）；10 月 2 日注册的不在范围内。
	idle := user("13900000001", "2026-09-30 16:30:00", "1.2")
	active := user("13900000002", "2026-10-01 02:00:00", "done")
	onboarded := user("13900000003", "2026-10-01 05:00:00", "done")
	user("13900000004", "2026-10-01 16:00:00", "1.1")

	sub := exec("INSERT INTO subjects (owner_user_id, name, code) VALUES (?, '中国语言文学基础', '654')", active)
	bank := exec("INSERT INTO banks (source, owner_user_id, subject_id) VALUES ('user', ?, ?)", active, sub)
	exec(`INSERT INTO materials (owner_user_id, bank_id, file_name, format, sha256, status, right_confirmed_at, created_at)
		VALUES (?, ?, '讲义', 'text', SHA2('a', 256), 'parsed', NOW(3), '2026-10-01 02:30:00')`, active, bank)
	sub3 := exec("INSERT INTO subjects (owner_user_id, name) VALUES (?, '文学概论')", onboarded)
	bank3 := exec("INSERT INTO banks (source, owner_user_id, subject_id) VALUES ('user', ?, ?)", onboarded, sub3)
	exec(`INSERT INTO materials (owner_user_id, bank_id, file_name, format, sha256, status, right_confirmed_at)
		VALUES (?, ?, '失败的资料', 'text', SHA2('b', 256), 'failed', NOW(3))`, onboarded, bank3)
	q := exec("INSERT INTO questions (bank_id, owner_user_id, qtype, stem, source, content_hash) VALUES (?, ?, 'term', '名词解释：SECRET-STEM', 'exam', SHA2('q', 256))", bank, active)
	answer := func(at string) uint64 {
		return exec("INSERT INTO attempts (owner_user_id, question_id, answer_mode, answer_text, answered_at) VALUES (?, ?, 'typed', 'SECRET-ANSWER', ?)", active, q, at)
	}
	first := answer("2026-10-01 03:00:00")
	answer("2026-10-01 15:30:00") // 北京时间还是 10 月 1 日 23:30，不算隔天
	answer("2026-10-01 16:30:00") // 北京时间 10 月 2 日 0:30
	answer("2026-10-09 03:00:00") // 注册满 7 天后
	exec("INSERT INTO attempts (owner_user_id, question_id, answer_mode, answer_text) VALUES (?, ?, 'typed', 'SECRET-ANSWER')", active, q)
	exec("INSERT INTO gradings (owner_user_id, attempt_id, kind, status, finished_at) VALUES (?, ?, 'subjective', 'done', '2026-10-01 03:01:00')", active, first)
	exec(`INSERT INTO orders (order_no, owner_user_id, tier, channel, amount_cents, status, paid_at)
		VALUES ('T-ACT-1', ?, 'monthly', 'wechat', 1900, 'paid', '2026-10-10 03:00:00')`, active)

	mk := func(name, phone string, roles []admin.Role) string {
		if _, err := b.Admin.CreateAdmin(ctx, name, name, phone, "Init-pass-2026", roles, false); err != nil {
			t.Fatal(err)
		}
		return f.login(name, phone, "Init-pass-2026")
	}
	analyst := mk("analyst1", "13700000002", []admin.Role{admin.RoleAnalyst})
	support := mk("support1", "13700000001", []admin.Role{admin.RoleSupport})

	code, body := f.do("GET", "/api/v1/admin/activation?from=2026-10-01&to=2026-10-01", analyst, "")
	var a gen.AdminActivation
	_ = json.Unmarshal([]byte(body), &a)
	if code != 200 || a.Total != 3 || len(a.Steps) != 8 || a.From.String() != "2026-10-01" || a.To.String() != "2026-10-01" {
		t.Fatalf("漏斗：%d %s", code, body)
	}
	want := map[gen.ActivationStage][2]int{ // 达成人数、停在这一级的人数
		"registered": {3, 1}, "onboarded": {2, 1}, "has_bank": {1, 0}, "first_answer": {1, 0},
		"first_grading": {1, 0}, "return_day": {1, 0}, "week_later": {1, 0}, "paid": {1, 1},
	}
	for _, s := range a.Steps {
		if w := want[s.Key]; s.Reached != w[0] || s.Stuck != w[1] {
			t.Errorf("%s：达成 %d、停留 %d，应为 %v", s.Key, s.Reached, s.Stuck, w)
		}
		if s.Key == "first_answer" && (s.MedianHours == nil || *s.MedianHours != 1) {
			t.Errorf("首次作答用时中位数应为 1 小时：%v", s.MedianHours)
		}
		if s.Key == "onboarded" && s.MedianHours != nil {
			t.Errorf("完成引导没有时间记录：%v", *s.MedianHours)
		}
	}
	if strings.Contains(body, "1390000") {
		t.Error("漏斗只有汇总，不含用户")
	}

	// 数据分析看不到具体用户；客服可以，手机号脱敏。
	if code, _ := f.do("GET", "/api/v1/admin/activation/users?from=2026-10-01&to=2026-10-01&stage=registered", analyst, ""); code != 403 {
		t.Errorf("数据分析看停留用户：%d", code)
	}
	code, body = f.do("GET", "/api/v1/admin/activation/users?from=2026-10-01&to=2026-10-01&stage=registered", support, "")
	var list struct {
		Items []gen.AdminJourney `json:"items"`
	}
	_ = json.Unmarshal([]byte(body), &list)
	if code != 200 || len(list.Items) != 1 || list.Items[0].UserId != int64(idle) || list.Items[0].PhoneMasked != "139****0001" || strings.Contains(body, "13900000001") {
		t.Errorf("停在注册的用户：%d %s", code, body)
	}
	_, body = f.do("GET", "/api/v1/admin/activation/users?from=2026-10-01&to=2026-10-01&stage=onboarded", support, "")
	if !strings.Contains(body, fmt.Sprintf(`"user_id":%d`, onboarded)) || strings.Contains(body, fmt.Sprintf(`"user_id":%d,`, active)) {
		t.Errorf("资料解析失败的停在完成引导：%s", body)
	}
	for _, p := range []string{
		"/api/v1/admin/activation/users?from=2026-10-01&to=2026-10-01&stage=nope",
		"/api/v1/admin/activation?from=2026-10-02&to=2026-10-01",
		"/api/v1/admin/activation?from=2025-01-01&to=2026-10-01",
	} {
		if code, _ := f.do("GET", p, support, ""); code != 400 {
			t.Errorf("%s：%d", p, code)
		}
	}

	// 用户详情里的使用进度。
	code, body = f.do("GET", fmt.Sprintf("/api/v1/admin/users/%d/journey", active), support, "")
	var j gen.AdminUserJourney
	_ = json.Unmarshal([]byte(body), &j)
	if code != 200 || j.Current != "paid" || len(j.Milestones) != 8 || j.ActiveDays30 != 1 || j.Answers30 != 1 || len(j.Days) != 1 {
		t.Fatalf("使用进度：%d %s", code, body)
	}
	if j.Days[0].Day.String() != time.Now().In(time.FixedZone("CST", 8*3600)).Format(time.DateOnly) {
		t.Errorf("按北京时间记日期：%v", j.Days[0].Day)
	}
	for _, m := range j.Milestones {
		if m.Key == "return_day" && (m.At == nil || !m.At.Equal(time.Date(2026, 10, 1, 16, 30, 0, 0, time.UTC))) {
			t.Errorf("隔天再练取北京时间第二天的第一次作答：%v", m.At)
		}
		if m.Key == "has_bank" && (m.At == nil || !m.At.Equal(time.Date(2026, 10, 1, 2, 30, 0, 0, time.UTC))) {
			t.Errorf("有题库取第一份解析成功的资料：%v", m.At)
		}
	}
	if j.LastAnswerAt == nil || j.LastActiveAt == nil || j.LastAnswerAt.Before(time.Now().Add(-time.Hour)) {
		t.Errorf("最后作答与最后活跃：%v %v", j.LastAnswerAt, j.LastActiveAt)
	}
	if strings.Contains(body, "SECRET") {
		t.Error("使用进度不含作答与题目内容")
	}
	if code, _ := f.do("GET", "/api/v1/admin/users/999999/journey", support, ""); code != 404 {
		t.Errorf("不存在的用户：%d", code)
	}
	if code, _ := f.do("GET", fmt.Sprintf("/api/v1/admin/users/%d/journey", active), analyst, ""); code != 403 {
		t.Errorf("数据分析看单个用户：%d", code)
	}
}

package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"peetraining-server/internal/gen"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/profile"
	"peetraining-server/internal/rules"
)

// TestIntegrationIDOR 是越权访问的全量检查（G4，CLAUDE.md 必须遵守第 4 条）：用户 B 拿用户 A 的 ID 调契约里每一个带路径参数的
// App 接口，必须返回 404。请求体按契约的 Schema 生成，保证校验能过、真正走到归属检查；新加的接口自动纳入检查。
func TestIntegrationIDOR(t *testing.T) {
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
	// 打开全部功能开关，避免「开关关闭返回 404」掩盖归属检查。
	if _, err := b.DB.Exec("UPDATE feature_flags SET enabled_for_all = 1"); err != nil {
		t.Fatal(err)
	}
	handler, err := b.Handler()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	f := &adminFx{t: t, b: b, srv: srv}

	login := func(phone, device string) (string, uint64) {
		if code, body := f.do("POST", "/api/v1/auth/sms-codes", "", `{"phone":"`+phone+`","purpose":"login","agree":true}`); code != 200 {
			t.Fatalf("发码：%d %s", code, body)
		}
		sms, _ := b.Cloud.SMS.(interface{ LastCode(string) (string, bool) }).LastCode(phone)
		_, body := f.do("POST", "/api/v1/auth/login", "", `{"phone":"`+phone+`","code":"`+sms+`","device":{"device_id":"`+device+`","platform":"ios"}}`)
		var l gen.LoginResponse
		_ = json.Unmarshal([]byte(body), &l)
		if l.AccessToken == "" {
			t.Fatalf("登录：%s", body)
		}
		var id uint64
		_ = b.DB.QueryRow("SELECT user_id FROM refresh_tokens WHERE device_id = ?", device).Scan(&id)
		return l.AccessToken, id
	}
	tokenA, a := login("13900005551", "device-a")
	tokenB, bID := login("13900005552", "device-b")
	if a == 0 || bID == 0 || a == bID {
		t.Fatalf("两个用户：%d %d", a, bID)
	}
	for _, u := range []uint64{a, bID} {
		if _, err := b.Profile.Upsert(ctx, u, profile.Input{ExamYear: 2027, Stage: rules.Strengthen, DailyMinutes: 45}); err != nil {
			t.Fatal(err)
		}
	}

	// 用户 A 的内容：每种路径参数都准备一个。
	sub, err := b.Profile.CreateSubject(ctx, a, profile.SubjectInput{Name: "中国语言文学基础", Code: ptrTo("654"), FullScore: 150})
	if err != nil {
		t.Fatal(err)
	}
	essaySub, err := b.Profile.CreateSubject(ctx, a, profile.SubjectInput{Name: "写作", FullScore: 150})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.DB.Exec("UPDATE subjects SET is_essay = 1 WHERE id = ?", essaySub.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Profile.CreateSubject(ctx, bID, profile.SubjectInput{Name: "中国语言文学基础", Code: ptrTo("654"), FullScore: 150}); err != nil {
		t.Fatal(err)
	}
	mat, err := b.Material.CreatePasted(ctx, a, sub.ID, "reference", "讲义", "第一章 文学理论\n意境：情景交融、虚实相生的艺术境界。", true)
	if err != nil {
		t.Fatal(err)
	}
	var bank, essayBank uint64
	_ = b.DB.QueryRow("SELECT bank_id FROM subjects WHERE id = ?", sub.ID).Scan(&bank)
	_ = b.DB.QueryRow("SELECT bank_id FROM subjects WHERE id = ?", essaySub.ID).Scan(&essayBank)
	if bank == 0 {
		_ = b.DB.QueryRow("SELECT id FROM banks WHERE owner_user_id = ? AND subject_id = ?", a, sub.ID).Scan(&bank)
		_ = b.DB.QueryRow("SELECT id FROM banks WHERE owner_user_id = ? AND subject_id = ?", a, essaySub.ID).Scan(&essayBank)
	}
	ins := func(q string, args ...any) uint64 {
		t.Helper()
		res, err := b.DB.Exec(q, args...)
		if err != nil {
			t.Fatalf("%s：%v", q, err)
		}
		id, _ := res.LastInsertId()
		return uint64(id)
	}
	job := ins("INSERT INTO import_jobs (owner_user_id, bank_id, mode) VALUES (?, ?, 'reference')", a, bank)
	ins("INSERT INTO import_job_materials (job_id, material_id, owner_user_id, status) VALUES (?, ?, ?, 'failed')", job, mat.ID, a)
	item := ins("INSERT INTO import_items (owner_user_id, job_id, item_type, payload) VALUES (?, ?, 'question', '{\"qtype\":\"term\",\"stem\":\"意境\"}')", a, job)
	kp := ins("INSERT INTO knowledge_points (owner_user_id, bank_id, level, name, original_text) VALUES (?, ?, 'point', '意境', '情景交融')", a, bank)
	kp2 := ins("INSERT INTO knowledge_points (owner_user_id, bank_id, level, name) VALUES (?, ?, 'point', '典型')", a, bank)
	rel := ins("INSERT INTO kp_relations (owner_user_id, bank_id, kp_a_id, kp_b_id, relation_type, origin) VALUES (?, ?, ?, ?, 'related', 'user_confirmed')", a, bank, kp, kp2)
	q := ins("INSERT INTO questions (owner_user_id, bank_id, qtype, stem, answer, score, source, content_hash) VALUES (?, ?, 'term', '名词解释：意境', '情景交融', 10, 'exam', SHA2('idor', 256))", a, bank)
	ins("INSERT INTO question_kps (question_id, kp_id, is_primary) VALUES (?, ?, 1)", q, kp)
	ins("INSERT INTO wrong_book (owner_user_id, question_id) VALUES (?, ?)", a, q)
	sess := ins("INSERT INTO practice_sessions (owner_user_id, subject_id, kind, question_ids) VALUES (?, ?, 'single', JSON_ARRAY(?))", a, sub.ID, q)
	recite := ins("INSERT INTO practice_sessions (owner_user_id, subject_id, kind, question_ids) VALUES (?, ?, 'recite', JSON_ARRAY())", a, sub.ID)
	att := ins("INSERT INTO attempts (owner_user_id, question_id, practice_session_id, answer_mode, answer_text) VALUES (?, ?, ?, 'typed', '情景交融')", a, q, sess)
	grading := ins("INSERT INTO gradings (owner_user_id, attempt_id, kind, status, score, full_score) VALUES (?, ?, 'subjective', 'done', 6, 10)", a, att)
	paper := ins("INSERT INTO papers (owner_user_id, bank_id, kind, title, full_score, actual_score, structure) VALUES (?, ?, 'real_exam', '2024 真题', 150, 150, JSON_ARRAY())", a, bank)
	paperSess := ins("INSERT INTO paper_sessions (owner_user_id, subject_id, paper_id, paper_kind, paper_title, mode, full_score) VALUES (?, ?, ?, 'real_exam', '2024 真题', 'practice', 150)", a, sub.ID, paper)
	essay := ins("INSERT INTO essays (owner_user_id, subject_id, topic_source, topic_text) VALUES (?, ?, 'custom', '守正与创新')", a, essaySub.ID)
	essayMat := ins("INSERT INTO essay_materials (owner_user_id, bank_id, theme, content, origin) VALUES (?, ?, '创新', '素材', 'user_confirmed')", a, essayBank)
	model := ins("INSERT INTO model_essays (owner_user_id, bank_id, title, content) VALUES (?, ?, '范文', '正文')", a, essayBank)
	export := ins("INSERT INTO export_jobs (owner_user_id, subject_id, options, format) VALUES (?, ?, JSON_OBJECT(), 'pdf')", a, sub.ID)
	msg := ins("INSERT INTO messages (owner_user_id, mtype, title, body) VALUES (?, 'announcement', '公告', '内容')", a)
	ins("INSERT INTO orders (owner_user_id, order_no, tier, channel, amount_cents) VALUES (?, 'T20261004000000123456', 'sprint', 'apple_iap', 16900)", a)

	values := map[string]string{
		"subjectId": fmt.Sprint(sub.ID), "materialId": fmt.Sprint(mat.ID), "jobId": fmt.Sprint(job), "itemId": fmt.Sprint(item), "kpId": fmt.Sprint(kp),
		"questionId": fmt.Sprint(q), "relationId": fmt.Sprint(rel), "gradingId": fmt.Sprint(grading), "paperId": fmt.Sprint(paper), "essayId": fmt.Sprint(essay),
		"essayMaterialId": fmt.Sprint(essayMat), "modelEssayId": fmt.Sprint(model), "exportId": fmt.Sprint(export), "messageId": fmt.Sprint(msg),
		"orderNo": "T20261004000000123456", "deviceId": "device-a", "pageNo": "1", "qtype": "term", "seq": "1",
	}
	// sessionId 在不同路径下是不同的会话。
	sessionFor := func(path string) string {
		switch {
		case strings.HasPrefix(path, "/recite-sessions"):
			return fmt.Sprint(recite)
		case strings.HasPrefix(path, "/paper-sessions"):
			return fmt.Sprint(paperSess)
		}
		return fmt.Sprint(sess)
	}
	// 作文课的接口用作文课。
	subjectFor := func(path string) string {
		if strings.Contains(path, "/essay") {
			return fmt.Sprint(essaySub.ID)
		}
		return fmt.Sprint(sub.ID)
	}

	spec, err := gen.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	reParam := regexp.MustCompile(`\{(\w+)\}`)
	var paths []string
	for p := range spec.Paths.Map() {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	checked := 0
	var failures []string
	for _, p := range paths {
		// 公共内容（协议正文）、渠道回调、后台接口不在这里检查（后台另有 TestIntegrationAdmin）。
		if !strings.Contains(p, "{") || strings.HasPrefix(p, "/admin") || strings.HasPrefix(p, "/pay/") || strings.HasPrefix(p, "/agreements/") || strings.HasPrefix(p, "/official-banks") {
			continue
		}
		for method, op := range p2ops(spec.Paths.Value(p)) {
			url := reParam.ReplaceAllStringFunc(p, func(m string) string {
				name := m[1 : len(m)-1]
				switch name {
				case "sessionId":
					return sessionFor(p)
				case "subjectId":
					return subjectFor(p)
				}
				v, ok := values[name]
				if !ok {
					t.Fatalf("%s %s：没有准备路径参数 %s 的值，在测试里补上", method, p, name)
				}
				return v
			})
			body := ""
			if op.RequestBody != nil && op.RequestBody.Value != nil {
				if mt := op.RequestBody.Value.Content.Get("application/json"); mt != nil && mt.Schema != nil {
					raw, _ := json.Marshal(sample(mt.Schema.Value, 0))
					body = string(raw)
				}
			}
			var query []string
			for _, pr := range op.Parameters {
				if pr.Value != nil && pr.Value.In == "query" && pr.Value.Required && pr.Value.Schema != nil {
					query = append(query, pr.Value.Name+"="+fmt.Sprint(sample(pr.Value.Schema.Value, 0)))
				}
			}
			if len(query) > 0 {
				url += "?" + strings.Join(query, "&")
			}
			code, resp := f.do(method, "/api/v1"+url, tokenB, body)
			checked++
			if code != 404 {
				failures = append(failures, fmt.Sprintf("%s %s → %d %s", method, p, code, trim(resp)))
			}
		}
	}
	if len(failures) > 0 {
		t.Errorf("用户 B 拿用户 A 的 ID 应一律 404（共查 %d 个接口）：\n%s", checked, strings.Join(failures, "\n"))
	}
	if checked < 80 {
		t.Errorf("检查的接口太少：%d", checked)
	}

	// 反过来确认这些 ID 对 A 是有效的（不是因为 ID 不存在才 404）。
	for _, u := range []string{"/subjects/" + values["subjectId"] + "/bank", "/questions/" + values["questionId"], "/knowledge-points/" + values["kpId"],
		"/materials/" + values["materialId"], "/gradings/" + values["gradingId"], "/import-items/" + values["itemId"], "/papers/" + values["paperId"],
		"/model-essays/" + values["modelEssayId"], "/exports/" + values["exportId"]} {
		if code, resp := f.do("GET", "/api/v1"+u, tokenA, ""); code != 200 {
			t.Errorf("A 自己访问 %s：%d %s", u, code, trim(resp))
		}
	}
	// B 的请求没有改动 A 的任何内容。
	var n int
	_ = b.DB.QueryRow("SELECT COUNT(*) FROM questions WHERE id = ? AND owner_user_id = ?", q, a).Scan(&n)
	if n != 1 {
		t.Error("A 的题目被 B 的请求改动了")
	}
	var revoked sql.NullTime
	_ = b.DB.QueryRow("SELECT revoked_at FROM refresh_tokens WHERE device_id = 'device-a'").Scan(&revoked)
	if revoked.Valid {
		t.Error("B 不能移除 A 的设备")
	}
}

func p2ops(item *openapi3.PathItem) map[string]*openapi3.Operation {
	out := map[string]*openapi3.Operation{}
	for m, op := range map[string]*openapi3.Operation{"GET": item.Get, "POST": item.Post, "PUT": item.Put, "PATCH": item.Patch, "DELETE": item.Delete} {
		if op != nil {
			out[m] = op
		}
	}
	return out
}

func trim(s string) string {
	if len(s) > 160 {
		return s[:160]
	}
	return s
}

// sample 按 Schema 生成一个能通过校验的值：必填字段、枚举第一个、满足长度与个数下限、常见格式。
func sample(s *openapi3.Schema, depth int) any {
	if s == nil || depth > 6 {
		return nil
	}
	if len(s.AllOf) > 0 {
		merged := map[string]any{}
		for _, sub := range s.AllOf {
			if m, ok := sample(sub.Value, depth+1).(map[string]any); ok {
				for k, v := range m {
					merged[k] = v
				}
			}
		}
		return merged
	}
	if len(s.OneOf) > 0 {
		return sample(s.OneOf[0].Value, depth+1)
	}
	if len(s.AnyOf) > 0 {
		return sample(s.AnyOf[0].Value, depth+1)
	}
	if len(s.Enum) > 0 {
		return s.Enum[0]
	}
	switch {
	case s.Type.Is("object") || len(s.Properties) > 0:
		out := map[string]any{}
		for _, name := range s.Required {
			if p := s.Properties[name]; p != nil {
				out[name] = sample(p.Value, depth+1)
			}
		}
		return out
	case s.Type.Is("array"):
		n := int(s.MinItems)
		items := make([]any, 0, n)
		for range n {
			items = append(items, sample(s.Items.Value, depth+1))
		}
		return items
	case s.Type.Is("integer"):
		if s.Min != nil && *s.Min > 1 {
			return int(*s.Min)
		}
		if s.Max == nil {
			// 像 ID 的整数用一个不存在的大数，避免碰巧等于路径里的 ID（「不能合并到自己」之类的校验）。
			return 987654321
		}
		return 1
	case s.Type.Is("number"):
		if s.Min != nil && *s.Min > 1 {
			return *s.Min
		}
		return 1
	case s.Type.Is("boolean"):
		return false
	}
	switch s.Format {
	case "date-time":
		return "2026-10-04T02:00:00Z"
	case "date":
		return "2026-10-04"
	case "uuid":
		return "00000000-0000-4000-8000-000000000000"
	}
	if s.Pattern != "" {
		if strings.Contains(s.Pattern, "1[3-9]") {
			return "13900000000"
		}
		return "a"
	}
	n := max(int(s.MinLength), 1)
	return strings.Repeat("字", n)
}

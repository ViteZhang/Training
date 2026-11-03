package practice_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/official"
	"peetraining-server/internal/params"
)

// TestCourseApplication 是 T46 的验收：申请收录归并、重复申请、达到阈值建议立项并推送企业微信（不含手机号）、
// 超 24 小时标红、立项 / 暂不收录通知所有申请人、官方题库上线通知申请人。
func TestCourseApplication(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	var mu sync.Mutex
	var pushes []string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		pushes = append(pushes, string(b))
		mu.Unlock()
	}))
	defer hook.Close()
	s := official.New(official.Deps{DB: f.db, AI: ai.NewEngine(ai.Config{Queries: dbq.New(f.db), UseMock: true}), Now: func() time.Time { return f.clock },
		Params: params.New(dbq.New(f.db)), OpsWebhook: hook.URL, AdminBaseURL: "https://example.test/admin"})

	u1, _ := f.user(t)
	u2, _ := f.user(t)
	u3, _ := f.user(t)
	if err := s.SetSeed(ctx, u2, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSeed(ctx, 999999, true); kind(err) != apperr.NotFound {
		t.Errorf("不存在的用户：%v", err)
	}
	in := official.ApplyInput{School: "四川大学", Major: "哲学", SubjectName: "西方哲学史", SubjectCode: "812", ExamYear: 2027, Entry: "onboarding"}
	r1, err := s.Apply(ctx, u1, in)
	if err != nil || r1.ApplicantCount != 1 {
		t.Fatalf("申请：%+v %v", r1, err)
	}
	if _, err := s.Apply(ctx, u1, in); kind(err) != apperr.Conflict {
		t.Errorf("重复申请：%v", err)
	}
	// 有代码时按代码归并：名称写法不同也算同一门课。
	in2 := in
	in2.SubjectName = "西方哲学"
	if r, err := s.Apply(ctx, u2, in2); err != nil || r.ApplicantCount != 2 {
		t.Fatalf("按代码归并：%+v %v", r, err)
	}
	gs, _ := s.ApplicationGroups(ctx, "", true)
	if len(gs) != 1 || gs[0].Status != "pending" || gs[0].SeedCount != 1 || gs[0].Discipline != "哲学" {
		t.Fatalf("待处理：%+v", gs)
	}
	// 第 3 人：建议立项，推送「建议立项」。
	if r, err := s.Apply(ctx, u3, in); err != nil || r.ApplicantCount != 3 {
		t.Fatalf("第 3 人：%+v %v", r, err)
	}
	gs, _ = s.ApplicationGroups(ctx, "哲学", false)
	if gs[0].Status != "suggested" {
		t.Errorf("达到 3 人建议立项：%+v", gs[0])
	}
	mu.Lock()
	n := len(pushes)
	last := ""
	if n > 0 {
		last = pushes[n-1]
	}
	mu.Unlock()
	if n != 3 || !strings.Contains(last, "建议立项") || strings.Contains(last, "1390000") {
		t.Errorf("企业微信推送：%d %s", n, last)
	}
	// 超过 24 小时未处理标红。
	f.clock = f.clock.Add(25 * time.Hour)
	st, err := s.ApplicationSummary(ctx)
	if err != nil || st.Pending != 1 || st.Suggested != 1 || st.Overdue != 1 {
		t.Errorf("统计：%+v %v", st, err)
	}
	apps, err := s.Applicants(ctx, gs[0].ID)
	if err != nil || len(apps) != 3 || !apps[1].Seed || apps[0].ExamYear != 2027 {
		t.Errorf("申请人：%+v %v", apps, err)
	}

	// 另一门课没有代码：暂不收录必须填原因；之后再有人申请重新进入待处理。
	other := official.ApplyInput{School: "南京大学", Major: "中国语言文学", SubjectName: "文学理论", ExamYear: 2027, Entry: "bank"}
	if _, err := s.Apply(ctx, u1, other); err != nil {
		t.Fatal(err)
	}
	gs, _ = s.ApplicationGroups(ctx, "", false)
	var nj official.ApplicationGroup
	for _, g := range gs {
		if g.School == "南京大学" {
			nj = g
		}
	}
	if err := s.RejectApplication(ctx, 0, nj.ID, ""); kind(err) != apperr.BadRequest {
		t.Errorf("暂不收录要填原因：%v", err)
	}
	if _, err := s.InitiateApplication(ctx, 0, nj.ID, "", ""); kind(err) != apperr.BadRequest {
		t.Errorf("没有代码不能立项：%v", err)
	}
	if err := s.RejectApplication(ctx, 0, nj.ID, "暂时没有足够的公开真题"); err != nil {
		t.Fatal(err)
	}
	if c := countMsgs(t, f, u1, "official_bank"); c != 1 {
		t.Errorf("暂不收录通知申请人：%d", c)
	}
	if _, err := s.Apply(ctx, u2, other); err != nil {
		t.Fatal(err)
	}
	gs, _ = s.ApplicationGroups(ctx, "", true)
	if len(gs) != 2 {
		t.Errorf("暂不收录的课有人再申请时重新待处理：%+v", gs)
	}

	// 立项：生成官方题库项目，通知所有申请人；上线时再通知一次。
	var west official.ApplicationGroup
	for _, g := range gs {
		if g.School == "四川大学" {
			west = g
		}
	}
	pid, err := s.InitiateApplication(ctx, 0, west.ID, "", "")
	if err != nil || pid == 0 {
		t.Fatalf("立项：%d %v", pid, err)
	}
	for _, u := range []uint64{u1, u2, u3} {
		var c int
		_ = f.db.QueryRow("SELECT COUNT(*) FROM messages WHERE owner_user_id = ? AND title LIKE '收录申请已立项%'", u).Scan(&c)
		if c != 1 {
			t.Errorf("立项通知 %d：%d", u, c)
		}
	}
	if _, err := s.InitiateApplication(ctx, 0, west.ID, "", ""); kind(err) != apperr.Conflict {
		t.Errorf("重复立项：%v", err)
	}
	p, err := s.Project(ctx, pid)
	if err != nil || p.SubjectCode != "812" || p.School != "四川大学" {
		t.Fatalf("立项生成的项目：%+v %v", p, err)
	}

	// 上线：直接在这个项目上发布（复用 T44 的发布流程，单独建一个 662 项目验证首次上线通知不受影响）。
	editor, lead := adminUser(t, f, "editor-apply"), adminUser(t, f, "lead-apply")
	if err := s.UpdateProject(ctx, pid, []uint64{editor}, nil, "producing"); err != nil {
		t.Fatal(err)
	}
	sc := 5.0
	rub := []official.Point{{Content: "要点一", Score: 3}, {Content: "要点二", Score: 2}}
	kp, _ := s.SaveDraft(ctx, editor, pid, 0, "knowledge_point", "add", 0, payload(official.KPPayload{Section: "近代哲学", Chapter: "康德", Name: "绝对命令", OriginalText: "绝对命令。", Rubric: rub}))
	q, _ := s.SaveDraft(ctx, editor, pid, 0, "question", "add", 0, payload(official.QuestionPayload{QType: "term", Stem: "名词解释：绝对命令", Answer: "无条件的道德律令。", Score: &sc, KPNames: []string{"绝对命令"}, Rubric: rub}))
	approveAll(t, s, editor, lead, pid, kp.ID, q.ID)
	if _, err := s.Publish(ctx, lead, pid, ""); err != nil {
		t.Fatal(err)
	}
	var launch int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM messages WHERE owner_user_id = ? AND title LIKE '官方题库上线%'", u3).Scan(&launch)
	if launch != 1 {
		t.Errorf("上线通知申请人：%d", launch)
	}

	// 我的申请。
	mine, err := s.MyApplications(ctx, u1)
	if err != nil || len(mine) != 2 {
		t.Errorf("我的申请：%+v %v", mine, err)
	}
}

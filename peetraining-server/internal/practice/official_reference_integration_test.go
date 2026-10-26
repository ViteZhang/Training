package practice_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/official"
)

// TestOfficialReferences 是 T36 的验收：7.11 录入有授权的素材并切分，7.12 生成官方题的参考答案时引用相关段落并标出处。
func TestOfficialReferences(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	s := official.New(official.Deps{DB: f.db, AI: ai.NewEngine(ai.Config{Queries: dbq.New(f.db), UseMock: true}), Now: func() time.Time { return f.clock }})
	lead := adminUser(t, f, "lead-ref")
	pid, err := s.CreateProject(ctx, lead, "某某大学", "中国古代文学", "654", "中国语言文学基础", nil)
	if err != nil {
		t.Fatal(err)
	}
	text := "第一章 文学活动\n文学活动由作品、作家、世界、读者四要素构成。\n第五章 审美范畴\n意境是情景交融、虚实相生、韵味无穷的艺术境界，是中国古典美学的核心范畴。"

	if _, err := s.CreateReference(ctx, lead, pid, "某教材", "textbook", false, "", text); !apperr.IsKind(err, apperr.BadRequest) {
		t.Errorf("没有授权不能录入：%v", err)
	}
	ref, err := s.CreateReference(ctx, lead, pid, "童庆炳《文学理论教程》第五版", "textbook", true, "出版社书面授权（2026-10）", text)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Segments != 2 {
		t.Errorf("应按章节切成 2 段：%+v", ref)
	}
	if _, err := s.CreateReference(ctx, lead, pid+999, "x", "handout", true, "授权", text); !apperr.IsKind(err, apperr.NotFound) {
		t.Errorf("项目不存在应返回 404：%v", err)
	}
	refs, err := s.References(ctx, pid)
	if err != nil || len(refs) != 1 || refs[0].Title != ref.Title {
		t.Fatalf("素材列表：%+v %v", refs, err)
	}

	a, err := s.AnswerCandidates(ctx, pid, "term", "名词解释：意境", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Citations) != 1 || a.Citations[0].Heading != "第五章 审美范畴" || a.Citations[0].ReferenceID != ref.ID {
		t.Errorf("应引用「审美范畴」那一段：%+v", a.Citations)
	}
	if !strings.Contains(a.Answer, "情景交融") || len(a.Points) == 0 {
		t.Errorf("答案应来自素材（mock 引用第一段原文）：%+v", a)
	}
	sum := 0.0
	for _, p := range a.Points {
		sum += p.Score
	}
	if sum != 5 {
		t.Errorf("采分点合计应等于分值：%v", sum)
	}
	// 素材里没有相关内容时不给出处。
	if a, err := s.AnswerCandidates(ctx, pid, "term", "名词解释：蒙太奇", 5); err != nil || len(a.Citations) != 0 {
		t.Errorf("无关题目不应引用：%+v %v", a.Citations, err)
	}

	if err := s.DeleteReference(ctx, pid, ref.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteReference(ctx, pid, ref.ID); !apperr.IsKind(err, apperr.NotFound) {
		t.Errorf("重复删除应返回 404：%v", err)
	}
	var n int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM official_reference_segments WHERE reference_id = ?", ref.ID).Scan(&n); err != nil || n != 0 {
		t.Errorf("段落应级联删除：%d %v", n, err)
	}
}

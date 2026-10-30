package practice_test

import (
	"context"
	"encoding/json"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/textbook"
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
	// T41：mock 没有独立语义/引文核验能力，不能把候选召回伪装为真实引用。
	if len(a.Citations) != 0 || a.Answer == "" || len(a.Points) == 0 {
		t.Fatalf("未核验不得宣称素材引用：%+v", a)
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

func TestProjectTextbooksAndPublishedAnswerSnapshots(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	s := official.New(official.Deps{DB: f.db, AI: ai.NewEngine(ai.Config{Queries: dbq.New(f.db), UseMock: true}), Now: func() time.Time { return f.clock }})
	lead, editor := adminUser(t, f, "link-lead"), adminUser(t, f, "link-editor")
	pid, err := s.CreateProject(ctx, lead, "测试院校", "文学", "654", "文学", []uint64{editor})
	if err != nil {
		t.Fatal(err)
	}
	catalog := textbook.New(f.db)
	book, err := catalog.Create(ctx, lead, "project-book-key", rules.TextbookMetadata{Title: "文学教材", Author: "作者", Kind: "textbook", Discipline: "文学"}, rules.TextbookEditionInput{Edition: "第五版", LicenseNote: "测试授权", Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	edition := book.Editions[0].ID
	if err = s.SetProjectTextbooks(ctx, pid, []uint64{edition}, 1, "link-unpublished"); !apperr.IsKind(err, apperr.Conflict) {
		t.Fatal("unpublished linked", err)
	}
	// 这里只验证关联可用性门禁，索引/发布本身由 textbook 集成测试覆盖。
	if _, err = f.db.Exec("UPDATE textbook_editions SET status='published',current_generation_id=1 WHERE id=?", edition); err != nil {
		t.Fatal(err)
	}
	if err = s.SetProjectTextbooks(ctx, pid, []uint64{edition}, 1, "link-published"); err != nil {
		t.Fatal(err)
	}
	if err = s.SetProjectTextbooks(ctx, pid, []uint64{edition}, 1, "link-published"); err != nil {
		t.Fatal("idempotent replay", err)
	}
	if err = s.SetProjectTextbooks(ctx, pid, nil, 1, "stale-editor-key"); !apperr.IsKind(err, apperr.Conflict) {
		t.Fatal("stale editor overwrote links", err)
	}
	if err = s.SetProjectTextbooks(ctx, pid, nil, 2, "unlink-published"); err != nil {
		t.Fatal(err)
	}
	if err = s.SetProjectTextbooks(ctx, pid, []uint64{edition}, 1, "link-published"); err != nil {
		t.Fatal(err)
	}
	rows, version, err := s.ProjectTextbooks(ctx, pid)
	if err != nil || len(rows) != 0 || version != 3 {
		t.Fatal("replay reverted newer links", rows, version, err)
	}
	if err = s.SetProjectTextbooks(ctx, pid, nil, 3, "link-published"); !apperr.IsKind(err, apperr.Conflict) {
		t.Fatal("changed same key", err)
	}
	if _, err = f.db.Exec("UPDATE textbook_editions SET authorized=0 WHERE id=?", edition); err != nil {
		t.Fatal(err)
	}
	if err = s.SetProjectTextbooks(ctx, pid, []uint64{edition}, 3, "link-revoked-key"); !apperr.IsKind(err, apperr.Conflict) {
		t.Fatal("revoked linked", err)
	}
	// Mock 只产通用知识快照；这里验证传递/归属，不以其替代真实教材引用质量评测。
	candidate, err := s.AnswerCandidates(ctx, pid, "term", "名词解释：意境", 5)
	if err != nil || candidate.SnapshotID == 0 {
		t.Fatal(candidate, err)
	}
	score := 5.0
	p := official.QuestionPayload{QType: "term", Stem: "名词解释：意境", Answer: candidate.Answer, Score: &score, Rubric: candidate.Points, KPNames: []string{}, AnswerSnapshotID: candidate.SnapshotID}
	draft, err := s.SaveDraft(ctx, editor, pid, 0, "question", "add", 0, payload(p))
	if err != nil {
		t.Fatal(err)
	}
	var saved official.QuestionPayload
	if err = json.Unmarshal(draft.Payload, &saved); err != nil || saved.AnswerSnapshotID != candidate.SnapshotID {
		t.Fatal("draft snapshot lost", err)
	}
	edited := p
	edited.Answer += "用户编辑"
	changed, err := s.SaveDraft(ctx, editor, pid, 0, "question", "add", 0, payload(edited))
	if err != nil {
		t.Fatal(err)
	}
	saved = official.QuestionPayload{}
	if err = json.Unmarshal(changed.Payload, &saved); err != nil || saved.AnswerSnapshotID != 0 {
		t.Fatal("edited content kept stale citation", err)
	}
	if err = s.DeleteDraft(ctx, editor, pid, changed.ID); err != nil {
		t.Fatal(err)
	}
	approveAll(t, s, editor, lead, pid, draft.ID)
	if _, err = s.Publish(ctx, lead, pid, "1.0"); err != nil {
		t.Fatal(err)
	}
	project, err := s.Project(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	var original uint64
	if err = f.db.QueryRow("SELECT answer_snapshot_id FROM questions WHERE bank_id=?", project.BankID).Scan(&original); err != nil || original != candidate.SnapshotID {
		t.Fatal("published snapshot lost", original, err)
	}
	uid, sid := f.user(t)
	if err = s.Subscribe(ctx, uid, project.BankID, sid); err != nil {
		t.Fatal(err)
	}
	var copied, owner uint64
	if err = f.db.QueryRow("SELECT s.id,s.owner_user_id FROM questions q JOIN answer_evidence_snapshots s ON s.id=q.answer_snapshot_id WHERE q.owner_user_id=? AND q.official_question_id IS NOT NULL", uid).Scan(&copied, &owner); err != nil || owner != uid || copied == original {
		t.Fatal("snapshot copy ownership", copied, owner, err)
	}
}

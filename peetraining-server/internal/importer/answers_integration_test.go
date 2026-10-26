package importer_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/importer"
	"peetraining-server/internal/quota"
)

// PRD v3 1.7、1.7c、11.16（10 月 26 日更新）：缺答案的题先从用户其他资料匹配，剩下的用答案文件配对。
func TestMissingAnswersMatchAndAnswerFile(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	var bank uint64
	if err := f.db.QueryRow("SELECT id FROM banks WHERE subject_id = ?", sid).Scan(&bank); err != nil {
		t.Fatal(err)
	}
	notes := f.upload(t, uid, sid, "pdf", "讲义\n典型：典型是共性与个性的统一。")
	// 用户其他资料：一个知识点（名词解释按名称匹配）与习题集里已有答案的同一道题。
	if _, err := f.db.Exec(`INSERT INTO knowledge_points (owner_user_id, bank_id, level, name, original_text, source_material_id, source_page, origin)
		VALUES (?, ?, 'point', '典型', '典型是共性与个性的统一。', ?, 1, 'imported')`, uid, bank, notes); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO questions (owner_user_id, bank_id, qtype, stem, answer, answer_origin, answer_match, answer_material_id, answer_page, source, content_hash)
		VALUES (?, ?, 'short_answer', '简述唐传奇的艺术成就。', '情节曲折、人物鲜明、语言华美。', 'imported', 'original', ?, 2, 'exercise', ?)`,
		uid, bank, notes, importer.ContentHash("简述唐传奇的艺术成就。")); err != nil {
		t.Fatal(err)
	}

	paper := f.upload(t, uid, sid, "pdf", examPaper)
	job, err := f.svc.CreateJob(ctx, uid, sid, "question", []uint64{paper})
	if err != nil {
		t.Fatal(err)
	}
	items, counts, err := f.svc.Items(ctx, uid, job.ID, "all", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	// 4 题都没有原文答案：「典型」和简答题从其他资料匹配到，「意境」和选择题还缺。
	if counts.AnswerMatched != 2 || counts.MissingAnswer != 2 {
		t.Fatalf("缺答案统计：%+v", counts)
	}
	byNo := map[string]importer.QuestionDraft{}
	for _, it := range items {
		var q importer.QuestionDraft
		_ = json.Unmarshal(it.Payload, &q)
		byNo[q.QuestionNo] = q
		if q.QuestionNo == "2" && (!it.NeedsReview || !strings.Contains(string(it.ReviewReasons), "answer_matched")) {
			t.Errorf("匹配到的答案要请用户核对：%s", it.ReviewReasons)
		}
	}
	if q := byNo["2"]; q.Answer != "典型是共性与个性的统一。" || q.AnswerMatch != importer.MatchOtherMaterial || q.AnswerMaterialID != notes || q.AnswerPage != 1 {
		t.Errorf("名词解释按知识点匹配：%+v", q)
	}
	if q := byNo["3"]; q.Answer != "情节曲折、人物鲜明、语言华美。" || q.AnswerMatch != importer.MatchOtherMaterial || q.AnswerPage != 2 {
		t.Errorf("同一道题沿用已有答案：%+v", q)
	}
	if byNo["1"].Answer != "" || byNo["4"].Answer != "" {
		t.Errorf("还缺答案：%+v %+v", byNo["1"], byNo["4"])
	}
	if miss, _, _ := f.svc.Items(ctx, uid, job.ID, "missing_answer", 0, 100); len(miss) != 4 {
		t.Errorf("「缺答案」筛选含已匹配待核对的：%d", len(miss))
	}
	if _, err := f.svc.Confirm(ctx, uid, job.ID, nil); err != nil {
		t.Fatal(err)
	}
	var match string
	var mid uint64
	_ = f.db.QueryRow("SELECT IFNULL(answer_match, ''), IFNULL(answer_material_id, 0) FROM questions WHERE owner_user_id = ? AND stem = '典型'", uid).Scan(&match, &mid)
	if match != "other_material" || mid != notes {
		t.Errorf("入库记下答案出处：%s %d", match, mid)
	}

	// 1.7c：补一份答案文件，按年份 + 题号配到题库里缺答案的题（意境、选择题）；简答题已有答案，答案 3 用不上。
	before := used(t, f, uid, quota.ParsePages)
	ans := f.upload(t, uid, sid, "pdf", answerFile)
	aj, err := f.svc.CreateAnswerJob(ctx, uid, sid, 0, []uint64{ans})
	if err != nil {
		t.Fatal(err)
	}
	if used(t, f, uid, quota.ParsePages) <= before {
		t.Error("答案文件计入解析额度")
	}
	view, err := f.svc.Pairings(ctx, uid, aj.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Pairings) != 2 || len(view.StillMissing) != 0 || len(view.Spare) != 1 {
		t.Fatalf("配对结果：%+v", view)
	}
	var choiceID uint64
	for _, p := range view.Pairings {
		if p.Method != "number" || p.Answer == nil {
			t.Errorf("按题号配上：%+v", p)
		}
		if p.Target.QType == "single_choice" {
			choiceID = p.ID
		}
	}
	// 取消选择题这一条；别人看不到也改不了。
	other, _ := f.user(t)
	if _, err := f.svc.Pairings(ctx, other, aj.ID); !apperr.IsKind(err, apperr.NotFound) {
		t.Errorf("别人的答案文件应 404：%v", err)
	}
	if err := f.svc.UpdatePairing(ctx, other, aj.ID, choiceID, 0); !apperr.IsKind(err, apperr.NotFound) {
		t.Errorf("改别人的配对应 404：%v", err)
	}
	if err := f.svc.UpdatePairing(ctx, uid, aj.ID, choiceID, 0); err != nil {
		t.Fatal(err)
	}
	n, err := f.svc.ConfirmPairings(ctx, uid, aj.ID)
	if err != nil || n != 1 {
		t.Fatalf("确认配对：%d %v", n, err)
	}
	var answer, origin string
	var rubric int
	_ = f.db.QueryRow("SELECT answer, answer_origin, answer_match FROM questions WHERE owner_user_id = ? AND stem = '意境'", uid).Scan(&answer, &origin, &match)
	_ = f.db.QueryRow("SELECT COUNT(*) FROM rubric_points r JOIN questions q ON q.id = r.question_id WHERE q.owner_user_id = ? AND q.stem = '意境' AND r.origin = 'ai_extracted'", uid).Scan(&rubric)
	if answer != "意境是情景交融、虚实相生的艺术境界。" || origin != "imported" || match != "answer_file" || rubric == 0 {
		t.Errorf("答案写进题目，来源为来自你的资料，采分点由 AI 提取待核对：%q %s %s %d", answer, origin, match, rubric)
	}
	if _, err := f.svc.ConfirmPairings(ctx, uid, aj.ID); !apperr.IsKind(err, apperr.Conflict) {
		t.Errorf("重复确认应 409：%v", err)
	}
}

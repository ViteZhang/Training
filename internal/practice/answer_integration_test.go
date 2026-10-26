package practice_test

import (
	"context"
	"strings"
	"testing"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/practice"
)

// PRD v3 4.4、4.7b、11.6、11.16（10 月 26 日更新）：还没有答案的题，提交时先生成 AI 参考答案再批改；
// 按 AI 参考答案批改的作答不计入预估分，确认答案后之后的批改计入，确认前的不追溯。
func TestAIReferenceAnswer(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	// 让「意境」与第一道选择题都没有答案，用户填了参考教材。
	if _, err := f.db.Exec("UPDATE subjects SET textbooks = JSON_ARRAY('童庆炳《文学理论教程》') WHERE id = ?", sid); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE questions SET answer = NULL, answer_origin = NULL WHERE owner_user_id = ? AND (stem = '意境' OR stem LIKE '下列属于唐传奇的是%')", uid); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("DELETE r FROM rubric_points r JOIN questions q ON q.id = r.question_id WHERE q.owner_user_id = ? AND q.stem = '意境'", uid); err != nil {
		t.Fatal(err)
	}
	aiUsed := func() int {
		var n int
		_ = f.db.QueryRow("SELECT IFNULL(SUM(used), 0) FROM quota_counters WHERE owner_user_id = ? AND quota_type = 'ai_questions'", uid).Scan(&n)
		return n
	}
	s := termSession(t, f, uid, sid)
	q := question(s, "意境")
	if q.ID == 0 || q.AnswerTier != "missing" {
		t.Fatalf("缺答案的题：%+v", q)
	}
	g, err := f.pr.SubmitSubjective(ctx, uid, s.ID, practice.SubjectiveInput{QuestionID: q.ID, Key: nextKey(), Answer: "意境是情景交融的艺术境界。"})
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != "done" || g.Rubric.AnswerTier != "ai" || !strings.Contains(g.Rubric.Reference, "据你的资料") || aiUsed() != 1 {
		t.Fatalf("先生成 AI 参考答案（贴近用户资料）再批改，计入 AI 出题额度：%+v 已用 %d", g.Rubric, aiUsed())
	}
	var origin string
	var ref int
	_ = f.db.QueryRow("SELECT answer_origin FROM questions WHERE id = ?", q.ID).Scan(&origin)
	_ = f.db.QueryRow("SELECT reference_only FROM attempts WHERE id = ?", g.AttemptID).Scan(&ref)
	if origin != "ai_generated" || ref != 1 {
		t.Fatalf("答案缓存在题目上，这次作答不计入预估分：%s %d", origin, ref)
	}
	// 再做一次不重复生成。
	g2, err := f.pr.SubmitSubjective(ctx, uid, s.ID, practice.SubjectiveInput{QuestionID: q.ID, Key: nextKey(), Answer: "意境"})
	if err != nil || g2.Rubric.AnswerTier != "ai" || aiUsed() != 1 {
		t.Fatalf("第二次作答沿用缓存的答案：%v %+v %d", err, g2.Rubric, aiUsed())
	}
	// 用户确认答案（3.3b、4.7b）：之后的批改计入，之前的不追溯。
	if _, err := f.db.Exec("UPDATE questions SET answer_origin = 'user_confirmed', ai_answer_outcome = 'confirmed' WHERE id = ?", q.ID); err != nil {
		t.Fatal(err)
	}
	g3, err := f.pr.SubmitSubjective(ctx, uid, s.ID, practice.SubjectiveInput{QuestionID: q.ID, Key: nextKey(), Answer: "意境是情景交融、虚实相生的艺术境界。"})
	if err != nil || g3.Rubric.AnswerTier != "confirmed" {
		t.Fatalf("确认后正常批改：%v %+v", err, g3.Rubric)
	}
	_ = f.db.QueryRow("SELECT reference_only FROM attempts WHERE id = ?", g.AttemptID).Scan(&ref)
	if ref != 1 {
		t.Error("确认前的批改不追溯计入")
	}
	if old, err := f.pr.Grading(ctx, uid, g.ID); err != nil || !old.AnswerConfirmed {
		t.Errorf("4.7b 重新打开：答案已确认，不再问「这份参考答案对吗？」：%v %+v", err, old.AnswerConfirmed)
	}

	// 客观题缺答案：作答时由 AI 给答案与解析，判分不计入预估分。
	cs, err := f.pr.Create(ctx, uid, practice.CreateInput{SubjectID: sid, Kind: practice.KindTypeDrill, QType: "single_choice"})
	if err != nil {
		t.Fatal(err)
	}
	c := choice(cs, "下列属于唐传奇的是")
	r, err := f.pr.Submit(ctx, uid, cs.ID, practice.AttemptInput{QuestionID: c.ID, Key: nextKey(), Selected: []string{"A"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.AnswerTier != "ai" || r.IsCorrect == nil || aiUsed() != 2 {
		t.Fatalf("客观题由 AI 给答案后判分：%+v %d", r, aiUsed())
	}
	_ = f.db.QueryRow("SELECT reference_only FROM attempts WHERE id = ?", r.AttemptID).Scan(&ref)
	if ref != 1 {
		t.Error("按 AI 答案判分的客观题不计入预估分")
	}
	// AI 答案被报错 3 次撤回，回到「缺答案」。
	for i := 0; i < 3; i++ {
		if _, err := f.pr.Report(ctx, uid, c.ID, "答案不对"); err != nil {
			t.Fatal(err)
		}
	}
	var answer *string
	_ = f.db.QueryRow("SELECT answer FROM questions WHERE id = ?", c.ID).Scan(&answer)
	if answer != nil {
		t.Errorf("报错 3 次后撤回 AI 答案：%v", *answer)
	}

	// AI 出题额度用完：没有答案的题不能生成，返回 402。
	if _, err := f.db.Exec("UPDATE questions SET answer = NULL, answer_origin = NULL WHERE id = ?", q.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("DELETE FROM rubric_points WHERE question_id = ?", q.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE quota_counters SET used = 100 WHERE owner_user_id = ? AND quota_type = 'ai_questions'", uid); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pr.SubmitSubjective(ctx, uid, s.ID, practice.SubjectiveInput{QuestionID: q.ID, Key: nextKey(), Answer: "意境"}); kind(err) != apperr.QuotaExceeded {
		t.Errorf("AI 出题额度用完应 402：%v", err)
	}
}

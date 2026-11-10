package practice_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/practice"
)

func enableFlags(t *testing.T, f *fx, uid uint64, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, err := f.db.Exec("INSERT INTO feature_flag_users (flag_key, user_id) VALUES (?, ?)", k, uid); err != nil {
			t.Fatal(err)
		}
	}
}

// TestTrial 是 T45 的验收：体验一道题不扣额度、次数以批改成功为准、加入题库保留题目采分点与作答、不保存全部删除；
// 通用体验题审核后才上线；AI 现场出题按专业课缓存。
func TestTrial(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	other, _ := f.user(t)

	// 开关关闭时 404。
	if _, err := f.pr.CreateTrial(ctx, uid, practice.TrialInput{QType: "term", Score: 5, Stem: "名词解释：意境"}); kind(err) != apperr.NotFound {
		t.Fatalf("trial 开关关闭：%v", err)
	}
	enableFlags(t, f, uid, "trial", "sample_trial", "ai_live_question")
	enableFlags(t, f, other, "trial", "sample_trial")

	// 1.9 识别：名词解释支持，选择题不支持。
	r, err := f.pr.RecognizeTrial(ctx, uid, "二、名词解释（每题 5 分）\n1. 意境")
	if err != nil || r.Stem == "" {
		t.Fatalf("识别：%+v %v", r, err)
	}
	if !r.Supported || r.QType != "term" || r.Score != 5 {
		t.Errorf("名词解释应可体验：%+v", r)
	}
	if _, err := f.pr.CreateTrial(ctx, uid, practice.TrialInput{QType: "single_choice", Score: 2, Stem: "下列属于唐传奇的是"}); kind(err) != apperr.BadRequest {
		t.Errorf("不支持的题型：%v", err)
	}

	// 没填参考答案：AI 生成，标 ai；提交前只给采分点个数。
	tr, err := f.pr.CreateTrial(ctx, uid, practice.TrialInput{InputMode: "paste", QType: "term", Score: 5, Stem: "名词解释：意境"})
	if err != nil {
		t.Fatal(err)
	}
	if tr.AnswerSource != "ai" || tr.PointCount == 0 || tr.Reference != "" || tr.Remaining != 1 {
		t.Fatalf("体验：%+v", tr)
	}
	// 别人拿不到。
	if _, err := f.pr.Trial(ctx, other, tr.ID); kind(err) != apperr.NotFound {
		t.Errorf("别人的体验 404：%v", err)
	}
	if _, err := f.pr.SubmitTrial(ctx, other, tr.ID, practice.TrialAnswer{Key: "other-trial-1", Answer: "x"}); kind(err) != apperr.NotFound {
		t.Errorf("提交别人的体验 404：%v", err)
	}

	// 批改：不扣额度，次数计 1；重复提交同一个键返回同一结果。
	g, err := f.pr.SubmitTrial(ctx, uid, tr.ID, practice.TrialAnswer{Key: "trial-key-0001", Answer: "意境是情景交融、虚实相生的艺术境界。"})
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != "graded" || g.Total == nil || len(g.Points) != tr.PointCount || g.Reference == "" || g.Remaining != 0 {
		t.Fatalf("批改结果：%+v", g)
	}
	if again, err := f.pr.SubmitTrial(ctx, uid, tr.ID, practice.TrialAnswer{Key: "trial-key-0001", Answer: "别的"}); err != nil || *again.Total != *g.Total {
		t.Errorf("幂等：%v", err)
	}
	var ledger int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM quota_ledger WHERE owner_user_id = ?", uid).Scan(&ledger)
	if ledger != 0 {
		t.Errorf("体验不扣任何额度：%d", ledger)
	}
	if st, _ := f.pr.TrialStatus(ctx, uid); st.Used != 1 || st.Remaining != 0 {
		t.Errorf("次数：%+v", st)
	}
	// 次数用完：通用体验题也不能再开始（共用次数）。
	if _, err := f.pr.CreateTrial(ctx, uid, practice.TrialInput{QType: "term", Score: 5, Stem: "名词解释：典型"}); kind(err) != apperr.Conflict {
		t.Errorf("次数用完：%v", err)
	}

	// 只加入这道题：题目、采分点、作答都保留，答案来源 AI 参考答案 · 待确认。
	if _, err := f.pr.SaveTrial(ctx, other, tr.ID, practice.TrialSaveOnly, sid); kind(err) != apperr.NotFound {
		t.Errorf("保存别人的体验 404：%v", err)
	}
	qid, err := f.pr.SaveTrial(ctx, uid, tr.ID, practice.TrialSaveOnly, sid)
	if err != nil || qid == 0 {
		t.Fatalf("加入题库：%d %v", qid, err)
	}
	var origin string
	var rubric, attempts, gradings int
	_ = f.db.QueryRow("SELECT answer_origin FROM questions WHERE id = ? AND owner_user_id = ?", qid, uid).Scan(&origin)
	_ = f.db.QueryRow("SELECT COUNT(*) FROM rubric_points WHERE question_id = ?", qid).Scan(&rubric)
	_ = f.db.QueryRow("SELECT COUNT(*) FROM attempts WHERE question_id = ?", qid).Scan(&attempts)
	_ = f.db.QueryRow("SELECT COUNT(*) FROM gradings g JOIN attempts a ON a.id = g.attempt_id WHERE a.question_id = ? AND g.quota_charged = 0", qid).Scan(&gradings)
	if origin != "ai_generated" || rubric != tr.PointCount || attempts != 1 || gradings != 1 {
		t.Errorf("加入题库后：origin=%s rubric=%d attempts=%d gradings=%d", origin, rubric, attempts, gradings)
	}
	if _, err := f.pr.SaveTrial(ctx, uid, tr.ID, practice.TrialSaveDiscard, 0); kind(err) != apperr.Conflict {
		t.Errorf("已加入的不能再删：%v", err)
	}

	// 另一个用户：填了参考答案（来自你的资料），不保存时全部删除，次数照算。
	_, osid := f.user(t)
	_ = osid
	ot, err := f.pr.CreateTrial(ctx, other, practice.TrialInput{QType: "short_answer", Score: 10, Stem: "简述典型的含义",
		Answer: "典型是共性与个性的统一。典型具有鲜明的个性特征。典型能反映社会生活的本质。"})
	if err != nil || ot.AnswerSource != "user" {
		t.Fatalf("填了参考答案：%+v %v", ot, err)
	}
	if _, err := f.pr.SubmitTrial(ctx, other, ot.ID, practice.TrialAnswer{Key: "trial-key-0002", Answer: "典型是共性与个性的统一。"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pr.SaveTrial(ctx, other, ot.ID, practice.TrialSaveDiscard, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pr.Trial(ctx, other, ot.ID); kind(err) != apperr.NotFound {
		t.Errorf("不保存后全部删除：%v", err)
	}
	if st, _ := f.pr.TrialStatus(ctx, other); st.Remaining != 0 {
		t.Errorf("删除后次数不回退：%+v", st)
	}

	// 7.18：手工录入进待审核，采分点合计须等于分值；上线后才在 1.10 显示。
	_, dirs, err := f.pr.SampleOverview(ctx)
	if err != nil || len(dirs) != 8 {
		t.Fatalf("学科方向：%d %v", len(dirs), err)
	}
	west := dirs[0].ID
	for _, d := range dirs {
		if d.Name == "西方哲学" {
			west = d.ID
		}
	}
	bad := practice.SampleInput{QType: "term", Score: 5, Stem: "绝对命令", Answer: "康德提出的无条件的道德律令。", Rubric: []ai.RubricPoint{{Content: "康德提出", Score: 2}, {Content: "无条件的道德律令", Score: 2}}}
	if _, err := f.pr.SaveSample(ctx, 0, west, 0, bad); kind(err) != apperr.BadRequest {
		t.Errorf("采分点合计不等于分值：%v", err)
	}
	bad.Rubric[1].Score = 3
	sqid, err := f.pr.SaveSample(ctx, 0, west, 0, bad)
	if err != nil {
		t.Fatal(err)
	}
	if cat, _ := f.pr.SampleCatalog(ctx); len(cat) != 0 {
		t.Errorf("待审核的不显示：%+v", cat)
	}
	if n, err := f.pr.GenerateSamples(ctx, 0, west, "term", 5, 2, "张志伟《西方哲学史》", nil); err != nil || n != 2 {
		t.Errorf("AI 批量生成：%d %v", n, err)
	}
	// 关联官方资料库里已上传的教材：参考教材记为书名，按方向检索原文。
	if _, err := f.pr.GenerateSamples(ctx, 0, west, "term", 5, 1, "", []uint64{7}); kind(err) != apperr.BadRequest {
		t.Errorf("没接资料库时关联教材要报错：%v", err)
	}
	mats := &fakeSampleMaterials{}
	f.pr.SetSampleMaterials(mats)
	if n, err := f.pr.GenerateSamples(ctx, 0, west, "short_answer", 10, 2, "", []uint64{7}); err != nil || n != 2 {
		t.Errorf("关联教材批量生成：%d %v", n, err)
	}
	if !slices.Equal(mats.ids, []uint64{7}) || !strings.Contains(mats.query, "西方哲学") {
		t.Errorf("检索参数：%v %q", mats.ids, mats.query)
	}
	var linked int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM sample_questions WHERE direction_id = ? AND textbook = ?", west, "西方哲学史 张志伟 第二版").Scan(&linked)
	if linked != 2 {
		t.Errorf("关联教材生成的题参考教材记为书名，实际 %d 道", linked)
	}
	mats.err = apperr.New(apperr.BadRequest, "所选教材暂不能用于出题")
	if _, err := f.pr.GenerateSamples(ctx, 0, west, "term", 5, 1, "", []uint64{8}); kind(err) != apperr.BadRequest {
		t.Errorf("教材不可用要报错：%v", err)
	}
	if err := f.pr.SetSampleStatus(ctx, 0, sqid, "online"); err != nil {
		t.Fatal(err)
	}
	cat, err := f.pr.SampleCatalog(ctx)
	if err != nil || len(cat) != 1 || cat[0].Name != "哲学" || len(cat[0].Directions) != 1 || len(cat[0].Directions[0].Questions) != 1 {
		t.Fatalf("1.10：%+v %v", cat, err)
	}

	// 新用户做通用体验题：被体验次数加 1。
	u3, s3 := f.user(t)
	enableFlags(t, f, u3, "sample_trial")
	st3, err := f.pr.CreateTrial(ctx, u3, practice.TrialInput{SampleQuestionID: sqid})
	if err != nil || st3.Kind != "sample" || st3.PointCount != 2 {
		t.Fatalf("通用体验题：%+v %v", st3, err)
	}
	if _, err := f.pr.SubmitTrial(ctx, u3, st3.ID, practice.TrialAnswer{Key: "trial-key-0003", Answer: "绝对命令是康德提出的无条件的道德律令。"}); err != nil {
		t.Fatal(err)
	}
	var tries int
	_ = f.db.QueryRow("SELECT try_count FROM sample_questions WHERE id = ?", sqid).Scan(&tries)
	if tries != 1 {
		t.Errorf("被体验次数：%d", tries)
	}
	if _, err := f.pr.SaveTrial(ctx, u3, st3.ID, practice.TrialSaveImport, s3); err != nil {
		t.Errorf("加入并导入：%v", err)
	}

	// AI 现场出题：同一门课缓存复用；没有 ai_live_question 开关的人不能做 AI 出题。
	l1, err := f.pr.LiveSample(ctx, uid, sid)
	if err != nil || !l1.AI {
		t.Fatalf("AI 现场出题：%+v %v", l1, err)
	}
	if l2, _ := f.pr.LiveSample(ctx, uid, sid); l2.ID != l1.ID {
		t.Errorf("同一门课缓存复用：%d %d", l1.ID, l2.ID)
	}
	if _, err := f.pr.LiveSample(ctx, other, sid); kind(err) != apperr.NotFound {
		t.Errorf("别人的专业课 404：%v", err)
	}
	u4, _ := f.user(t)
	enableFlags(t, f, u4, "sample_trial")
	if _, err := f.pr.CreateTrial(ctx, u4, practice.TrialInput{SampleQuestionID: l1.ID}); kind(err) != apperr.NotFound {
		t.Errorf("AI 出题受 ai_live_question 开关控制：%v", err)
	}
	// 抽检下线后，同一门课下次重新生成。
	if err := f.pr.SetSampleStatus(ctx, 0, l1.ID, "offline"); err != nil {
		t.Fatal(err)
	}
	if l3, err := f.pr.LiveSample(ctx, uid, sid); err != nil || l3.ID == l1.ID {
		t.Errorf("下线后重新生成：%+v %v", l3, err)
	}
}

type fakeSampleMaterials struct {
	ids   []uint64
	query string
	err   error
}

func (m *fakeSampleMaterials) SampleMaterial(_ context.Context, ids []uint64, query string) ([]string, []string, error) {
	m.ids, m.query = ids, query
	if m.err != nil {
		return nil, nil, m.err
	}
	return []string{"西方哲学史 张志伟 第二版"}, []string{"【西方哲学史】康德的绝对命令是无条件的道德律令。"}, nil
}

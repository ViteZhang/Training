package practice_test

import (
	"context"
	"math"
	"testing"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/practice"
)

// mockPaper 做完一套模拟考试：第 1 题答对、第 2 题答错、第 3 题作答、第 4 题空着，用时 100 分钟。
func mockPaper(t *testing.T, f *fx, uid, sid uint64) practice.PaperSessionView {
	t.Helper()
	ctx := context.Background()
	pb := realPaper(t, f, uid, sid)
	v, err := f.pr.StartPaper(ctx, uid, pb.ID, "mock", nextKey())
	if err != nil {
		t.Fatal(err)
	}
	answers := map[string]string{"下列属于唐传奇的是": "A", "《文心雕龙》的作者是": "A", "意境": "意境是情景交融、虚实相生的艺术境界。"}
	for _, it := range v.Items {
		if a, ok := answers[it.Stem]; ok {
			if _, err := f.pr.UpdatePaperItem(ctx, uid, v.ID, it.Seq, practice.ItemUpdate{Draft: &a, TimeDelta: 300}); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.clock = f.clock.Add(100 * time.Minute)
	v, err = f.pr.SubmitPaper(ctx, uid, v.ID)
	if err != nil || v.Status != "graded" || v.Score == nil {
		t.Fatalf("交卷：%+v %v", v.Status, err)
	}
	return v
}

func estimates(f *fx, uid uint64) (n int, reasons []string) {
	rows, err := f.db.Query("SELECT trigger_reason FROM score_estimates WHERE owner_user_id = ? ORDER BY id", uid)
	if err != nil {
		return 0, nil
	}
	defer rows.Close()
	for rows.Next() {
		var r string
		_ = rows.Scan(&r)
		reasons = append(reasons, r)
	}
	if rows.Err() != nil {
		return 0, nil
	}
	return len(reasons), reasons
}

func TestEstimateAfterRealPaper(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)

	cards, err := f.sc.Cards(ctx, uid)
	if err != nil || len(cards) != 1 || cards[0].Ready {
		t.Fatalf("没做整卷时不显示预估分：%+v %v", cards, err)
	}
	v := mockPaper(t, f, uid, sid)
	score := *v.Score

	// 只做过 1 套、各题型近期作答都不足 5 题：实测分与模型分都等于这套卷的得分，区间 ±10%（PRD 11.6）。
	cards, err = f.sc.Cards(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	c := cards[0]
	if !c.Ready || c.BasisPapers != 1 {
		t.Errorf("卡片：%+v", c)
	}
	if c.Low != int(math.Round(score*0.9)) || c.High != int(math.Round(score*1.1)) {
		t.Errorf("预估分区间应为 %v ×(1±10%%)：%d–%d", score, c.Low, c.High)
	}
	if c.BasisQuestions != 1 || c.MainGap == "" || c.Gap != nil || c.TodayChange != nil {
		t.Errorf("依据与主要差在：%+v", c)
	}

	// AI 组卷成绩不计入：一套 AI 卷满分也不改变预估分。
	if _, err := f.db.Exec(`INSERT INTO paper_sessions (owner_user_id, subject_id, paper_kind, paper_title, mode, status, started_at, submitted_at, graded_at,
		full_score, score, counts_for_estimate) VALUES (?, ?, 'ai_standard', 'AI 标准卷', 'practice', 'graded', ?, ?, ?, 150, 150, 0)`, uid, sid, f.clock, f.clock, f.clock); err != nil {
		t.Fatal(err)
	}
	if err := f.sc.Recompute(ctx, uid, sid, "test"); err != nil {
		t.Fatal(err)
	}
	cards, _ = f.sc.Cards(ctx, uid)
	if cards[0].Low != c.Low || cards[0].High != c.High {
		t.Errorf("AI 组卷不计入：%+v", cards[0])
	}

	// 设了目标分后显示差距；删除资料触发重算。
	target := 120
	if _, err := f.db.Exec("UPDATE subjects SET target_score = ? WHERE id = ?", target, sid); err != nil {
		t.Fatal(err)
	}
	var mid uint64
	_ = f.db.QueryRow("SELECT m.id FROM materials m JOIN banks b ON b.id = m.bank_id WHERE m.owner_user_id = ? AND b.subject_id = ? ORDER BY m.id DESC LIMIT 1", uid, sid).Scan(&mid)
	if err := f.mat.Delete(ctx, uid, mid); err != nil {
		t.Fatal(err)
	}
	if _, reasons := estimates(f, uid); reasons[0] != "paper_graded" || reasons[len(reasons)-1] != "material_deleted" {
		t.Errorf("重算时机：%v", reasons)
	}
	cards, _ = f.sc.Cards(ctx, uid)
	if g := cards[0].Gap; g == nil || *g != max(target-cards[0].High, 0) {
		t.Errorf("差距 = 目标 − 预估上限：%+v", cards[0])
	}

	// 整卷报告与时间分析。
	r, err := f.sc.PaperReport(ctx, uid, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Score != score || !r.CountsForEstimate || r.PrevDelta != nil || r.Gap == nil || len(r.ByQType) != 2 || r.WeakestQType == "" {
		t.Errorf("整卷报告：%+v", r)
	}
	if r.Time == nil || r.Time.Unanswered != 1 || r.Time.UsedMinutes != 100 || r.Time.UsedFull || len(r.Time.Sections) != 2 || len(r.Time.Trend) != 1 ||
		r.Time.Conclusion == "" || len(r.Time.Advice) == 0 || r.Time.CheckSuggestedMinutes != 15 {
		t.Errorf("时间分析：%+v", r.Time)
	}
	for _, s := range r.Time.Sections {
		if s.QType == "term" && !s.Unfinished {
			t.Errorf("名词解释有 1 题没写：%+v", s)
		}
	}

	// 提分看板。
	d, err := f.sc.Dashboard(ctx, uid, sid)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Card.Ready || len(d.Trend) != 1 || len(d.RecentPapers) != 2 || d.RecentPapers[0].CountsForEstimate {
		t.Errorf("看板：%+v", d)
	}
	if d.Trend[0].Low != cards[0].Low {
		t.Errorf("本周取最后一次：%+v", d.Trend)
	}

	// 第二天再重算：分数没变时不显示「今天的变化」；变了才显示。
	f.clock = f.clock.Add(24 * time.Hour)
	if _, err := f.db.Exec("UPDATE paper_sessions SET score = score + 10 WHERE id = ?", v.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.sc.Recompute(ctx, uid, sid, "test"); err != nil {
		t.Fatal(err)
	}
	cards, _ = f.sc.Cards(ctx, uid)
	if cards[0].TodayChange == nil || *cards[0].TodayChange != 6 {
		t.Errorf("今天的变化 = 0.6 × 10：%+v", cards[0].TodayChange)
	}

	// 别人的整卷报告与看板返回 404。
	other, osid := f.user(t)
	if _, err := f.sc.PaperReport(ctx, other, v.ID); kind(err) != apperr.NotFound {
		t.Errorf("别人的报告：%v", err)
	}
	if _, err := f.sc.Dashboard(ctx, other, sid); kind(err) != apperr.NotFound {
		t.Errorf("别人的看板：%v", err)
	}
	if d, err := f.sc.Dashboard(ctx, other, osid); err != nil || d.Card.Ready {
		t.Errorf("新用户看板：%+v %v", d.Card, err)
	}
}

func TestFalseMasteryToToday(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	if n, err := f.plan.AddFalseMastery(ctx, uid, sid); err != nil || n != 0 {
		t.Fatalf("没有「以为会了」时不加：%d %v", n, err)
	}
	// 自评掌握、但近 7 天两次都答错的知识点。
	var qid, kp uint64
	if err := f.db.QueryRow(`SELECT q.id, qk.kp_id FROM questions q JOIN question_kps qk ON qk.question_id = q.id
		WHERE q.owner_user_id = ? AND q.stem LIKE '典型%' LIMIT 1`, uid).Scan(&qid, &kp); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO kp_mastery (owner_user_id, kp_id, m, state, last_self_assess) VALUES (?, ?, 85, 'mastered', 'mastered')
		ON DUPLICATE KEY UPDATE last_self_assess = 'mastered'`, uid, kp); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := f.db.Exec(`INSERT INTO attempts (owner_user_id, question_id, answer_mode, answer_text, score, full_score, answered_at)
			VALUES (?, ?, 'typed', 'x', 1, 10, ?)`, uid, qid, f.clock); err != nil {
			t.Fatal(err)
		}
	}
	kps, err := f.plan.FalseMasteryKPs(ctx, uid, sid)
	if err != nil || len(kps) != 1 || kps[0].ID != kp {
		t.Fatalf("以为会了：%+v %v", kps, err)
	}
	if _, err := f.plan.AddFalseMastery(ctx, uid, sid); err != nil {
		t.Fatal(err)
	}
	p, err := f.plan.Today(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range p.Items {
		if it.KPID == kp {
			found = true
		}
	}
	if !found {
		t.Errorf("今日计划里应有这个知识点的题：%+v", p.Items)
	}
	// 再加一次不重复加题。
	before := len(p.Items)
	if n, err := f.plan.AddFalseMastery(ctx, uid, sid); err != nil || n != 0 {
		t.Errorf("重复加：%d %v", n, err)
	}
	if p, _ = f.plan.Today(ctx, uid); len(p.Items) != before {
		t.Errorf("题数不变：%d → %d", before, len(p.Items))
	}
	other, _ := f.user(t)
	if _, err := f.plan.AddFalseMastery(ctx, other, sid); kind(err) != apperr.NotFound {
		t.Errorf("别人的课：%v", err)
	}
}

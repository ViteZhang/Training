package practice_test

import (
	"context"
	"testing"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/practice"
)

func realPaper(t *testing.T, f *fx, uid, sid uint64) practice.PaperBrief {
	t.Helper()
	l, err := f.pr.Papers(context.Background(), uid, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Real) == 0 {
		t.Fatalf("导入真题后应有真题卷：%+v", l)
	}
	return l.Real[0]
}

func used(f *fx, uid uint64, typ string) int {
	var n int
	_ = f.db.QueryRow("SELECT COALESCE(SUM(used), 0) FROM quota_counters WHERE owner_user_id = ? AND quota_type = ?", uid, typ).Scan(&n)
	return n
}

func TestPaperPracticeFlow(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	pb := realPaper(t, f, uid, sid)
	if pb.Status != "not_started" || pb.QuestionCount != 4 || pb.ExamYear == nil || *pb.ExamYear != 2024 {
		t.Fatalf("真题卷：%+v", pb)
	}
	d, err := f.pr.Paper(ctx, uid, pb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Sections) == 0 || d.Sections[0].SuggestedMinutes <= 0 || d.RecommendedMode != "practice" || !d.CountsForEstimate || d.CheckMinutes != 15 {
		t.Errorf("选择模式：%+v", d)
	}

	key := nextKey()
	v, err := f.pr.StartPaper(ctx, uid, pb.ID, "practice", key)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := f.pr.StartPaper(ctx, uid, pb.ID, "practice", key); err != nil || again.ID != v.ID {
		t.Errorf("幂等：%v", err)
	}
	if _, err := f.pr.StartPaper(ctx, uid, pb.ID, "mock", nextKey()); kind(err) != apperr.Conflict {
		t.Errorf("同一时间只能一套进行中：%v", err)
	}
	if v.Deadline != nil || len(v.Items) != 4 || len(v.Reminders) == 0 {
		t.Fatalf("练习模式：%+v", v)
	}
	for _, it := range v.Items {
		if it.Stem == "" {
			t.Error("作答页要有题干")
		}
	}
	answers := map[string]string{"下列属于唐传奇的是": "A", "《文心雕龙》的作者是": "A", "意境": "意境是情景交融、虚实相生的艺术境界。"}
	for _, it := range v.Items {
		if a, ok := answers[it.Stem]; ok {
			if _, err := f.pr.UpdatePaperItem(ctx, uid, v.ID, it.Seq, practice.ItemUpdate{Draft: &a, TimeDelta: 60}); err != nil {
				t.Fatal(err)
			}
		}
	}
	mark := true
	if it, err := f.pr.UpdatePaperItem(ctx, uid, v.ID, 4, practice.ItemUpdate{Marked: &mark, TimeDelta: 30}); err != nil || !it.Marked {
		t.Errorf("标记：%+v %v", it, err)
	}
	// 练习模式可暂停，暂停时不计用时。
	if v, err = f.pr.PausePaper(ctx, uid, v.ID); err != nil || v.Status != "paused" {
		t.Fatalf("暂停：%+v %v", v.Status, err)
	}
	f.clock = f.clock.Add(10 * time.Minute)
	if it, _ := f.pr.UpdatePaperItem(ctx, uid, v.ID, 4, practice.ItemUpdate{TimeDelta: 30}); it.TimeSpent != 30 {
		t.Errorf("暂停时不计用时：%d", it.TimeSpent)
	}
	if v, err = f.pr.ResumePaper(ctx, uid, v.ID, nil); err != nil || v.Status != "in_progress" {
		t.Fatalf("继续：%v", err)
	}
	if v.ElapsedSeconds != 0 {
		t.Errorf("暂停的 10 分钟不算已用时间：%d", v.ElapsedSeconds)
	}
	if used(f, uid, "paper_grading") != 0 {
		t.Error("开始时只预占，交卷才结算")
	}

	v, err = f.pr.SubmitPaper(ctx, uid, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 测试里的队列同步执行：交卷后客观题即时判分，主观题已批改完。
	if v.Status != "graded" || v.Score == nil {
		t.Fatalf("交卷：%+v", v)
	}
	got := map[string]float64{}
	for _, it := range v.Items {
		if it.Got != nil {
			got[it.Stem] = *it.Got
		}
	}
	if got["下列属于唐传奇的是"] <= 0 || got["《文心雕龙》的作者是"] != 0 || got["意境"] <= 0 {
		t.Errorf("逐题得分：%v", got)
	}
	if _, ok := got["典型"]; ok {
		t.Error("没作答的题不批改")
	}
	if used(f, uid, "paper_grading") != 1 {
		t.Errorf("整卷批改次数：%d", used(f, uid, "paper_grading"))
	}
	var msgs int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM messages WHERE owner_user_id = ? AND mtype = 'paper_graded'", uid).Scan(&msgs)
	if msgs != 1 {
		t.Errorf("批改完成发消息：%d", msgs)
	}
	var report string
	_ = f.db.QueryRow("SELECT CAST(report AS CHAR) FROM paper_sessions WHERE id = ?", v.ID).Scan(&report)
	if report == "" {
		t.Error("应保存整卷统计")
	}
	if again, err := f.pr.SubmitPaper(ctx, uid, v.ID); err != nil || *again.Score != *v.Score {
		t.Errorf("重复交卷：%v", err)
	}
	if _, err := f.pr.UpdatePaperItem(ctx, uid, v.ID, 1, practice.ItemUpdate{TimeDelta: 1}); kind(err) != apperr.Conflict {
		t.Errorf("交卷后不能再改：%v", err)
	}
	pb = realPaper(t, f, uid, sid)
	if pb.Status != "done" || pb.Last == nil || pb.Last.Score == nil {
		t.Errorf("列表状态：%+v", pb)
	}
	// 免费版每周 1 套：再开始、再组卷都提示次数用完。
	if _, err := f.pr.StartPaper(ctx, uid, pb.ID, "practice", nextKey()); kind(err) != apperr.QuotaExceeded {
		t.Errorf("每周 1 套：%v", err)
	}
	if _, err := f.pr.ComposePaper(ctx, uid, sid, "ai_standard"); kind(err) != apperr.QuotaExceeded {
		t.Errorf("组卷也要有次数：%v", err)
	}
}

func TestMockDeadlineAndRecover(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	pb := realPaper(t, f, uid, sid)

	// 放弃退回预占的次数。
	v, err := f.pr.StartPaper(ctx, uid, pb.ID, "mock", nextKey())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pr.PausePaper(ctx, uid, v.ID); kind(err) != apperr.Conflict {
		t.Errorf("模拟考试不能暂停：%v", err)
	}
	if err := f.pr.AbandonPaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	if used(f, uid, "paper_grading") != 0 {
		t.Error("放弃应退回次数")
	}

	v, err = f.pr.StartPaper(ctx, uid, pb.ID, "mock", nextKey())
	if err != nil {
		t.Fatal(err)
	}
	if v.Deadline == nil || !v.Deadline.Equal(f.clock.Add(180*time.Minute)) || !v.ResumeAvailable {
		t.Fatalf("模拟考试截止时间以服务端为准：%+v", v.Deadline)
	}
	// 中断超过 10 分钟不能恢复；10 分钟内恢复一次、补回中断时长；第二次不行。
	f.clock = f.clock.Add(30 * time.Minute)
	old := f.clock.Add(-11 * time.Minute)
	if _, err := f.pr.ResumePaper(ctx, uid, v.ID, &old); kind(err) != apperr.Conflict {
		t.Errorf("超过 10 分钟：%v", err)
	}
	at := f.clock.Add(-5 * time.Minute)
	v, err = f.pr.ResumePaper(ctx, uid, v.ID, &at)
	if err != nil || !v.Deadline.Equal(f.clock.Add(155*time.Minute)) || v.ResumeAvailable {
		t.Fatalf("恢复补回 5 分钟：%+v %v", v.Deadline, err)
	}
	if _, err := f.pr.ResumePaper(ctx, uid, v.ID, &at); kind(err) != apperr.Conflict {
		t.Errorf("只能恢复一次：%v", err)
	}
	a := "意境是情景交融的境界。"
	if _, err := f.pr.UpdatePaperItem(ctx, uid, v.ID, 3, practice.ItemUpdate{Draft: &a, TimeDelta: 120}); err != nil {
		t.Fatal(err)
	}
	// 截止时间的任务提前执行不交卷；时间到后自动交卷（打开作答页或任务执行时），按截止时间记。
	if err := f.pr.AutoSubmitPaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	if cur, _ := f.pr.PaperSession(ctx, uid, v.ID); cur.Status != "in_progress" {
		t.Fatalf("还没到时间：%s", cur.Status)
	}
	deadline := *v.Deadline
	f.clock = deadline.Add(time.Minute)
	if _, err := f.pr.UpdatePaperItem(ctx, uid, v.ID, 3, practice.ItemUpdate{Draft: &a}); kind(err) != apperr.Conflict {
		t.Errorf("过了截止时间不能再改：%v", err)
	}
	cur, err := f.pr.PaperSession(ctx, uid, v.ID)
	if err != nil || cur.Status != "graded" {
		t.Fatalf("时间到自动交卷并批改：%+v %v", cur.Status, err)
	}
	var submitted time.Time
	_ = f.db.QueryRow("SELECT submitted_at FROM paper_sessions WHERE id = ?", v.ID).Scan(&submitted)
	if !submitted.Equal(deadline) {
		t.Errorf("按截止时间交卷：%v vs %v", submitted, deadline)
	}
	var timed bool
	_ = f.db.QueryRow("SELECT timed FROM attempts WHERE paper_session_id = ? LIMIT 1", v.ID).Scan(&timed)
	if !timed {
		t.Error("模拟考试的作答记为限时作答")
	}
}

func TestComposePaper(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	real := realPaper(t, f, uid, sid)
	for _, kind := range []string{"ai_standard", "ai_targeted"} {
		d, err := f.pr.ComposePaper(ctx, uid, sid, kind)
		if err != nil {
			t.Fatal(err)
		}
		if d.Kind != kind || d.QuestionCount != real.QuestionCount || d.CountsForEstimate || len(d.Sections) == 0 {
			t.Errorf("%s：%+v", kind, d)
		}
	}
	if used(f, uid, "ai_questions") != 0 {
		t.Error("组卷补的变式题不扣 AI 出题额度")
	}
	l, _ := f.pr.Papers(ctx, uid, sid)
	if len(l.AI) != 2 {
		t.Errorf("AI 组卷列表：%d", len(l.AI))
	}
	if _, err := f.pr.ComposePaper(ctx, uid, sid, "official_mock"); kind(err) != apperr.BadRequest {
		t.Errorf("组卷类型：%v", err)
	}
	// 没导入真题卷不能组卷。
	other, osid := f.user(t)
	if _, err := f.pr.ComposePaper(ctx, other, osid, "ai_standard"); kind(err) != apperr.BadRequest {
		t.Errorf("没有真题卷：%v", err)
	}
}

func TestPaperOwnership(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	a, sa := f.user(t)
	b, sb := f.user(t)
	f.imported(t, a, sa)
	pb := realPaper(t, f, a, sa)
	v, err := f.pr.StartPaper(ctx, a, pb.ID, "practice", nextKey())
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, err error) {
		t.Helper()
		if kind(err) != apperr.NotFound {
			t.Errorf("%s：拿别人的 ID 应返回 404，got %v", name, err)
		}
	}
	_, err = f.pr.Papers(ctx, b, sa)
	check("整卷列表", err)
	_, err = f.pr.Paper(ctx, b, pb.ID)
	check("试卷", err)
	_, err = f.pr.StartPaper(ctx, b, pb.ID, "practice", nextKey())
	check("开始", err)
	_, err = f.pr.PaperSession(ctx, b, v.ID)
	check("作答", err)
	_, err = f.pr.UpdatePaperItem(ctx, b, v.ID, 1, practice.ItemUpdate{})
	check("草稿", err)
	_, err = f.pr.PausePaper(ctx, b, v.ID)
	check("暂停", err)
	_, err = f.pr.ResumePaper(ctx, b, v.ID, nil)
	check("继续", err)
	_, err = f.pr.SubmitPaper(ctx, b, v.ID)
	check("交卷", err)
	check("放弃", f.pr.AbandonPaper(ctx, b, v.ID))
	_, err = f.pr.ComposePaper(ctx, b, sa, "ai_standard")
	check("组卷", err)
	if l, err := f.pr.Papers(ctx, b, sb); err != nil || len(l.Real) != 0 || l.InProgress != nil {
		t.Errorf("自己的列表看不到别人的卷：%+v %v", l, err)
	}
}

package practice_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"peetraining-server/internal/admin"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/params"
	"peetraining-server/internal/practice"
)

func termSession(t *testing.T, f *fx, uid, sid uint64) practice.Session {
	t.Helper()
	s, err := f.pr.Create(context.Background(), uid, practice.CreateInput{SubjectID: sid, Kind: practice.KindTypeDrill, QType: "term"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func question(s practice.Session, stem string) practice.Question {
	for _, q := range s.Questions {
		if q.Stem == stem {
			return q
		}
	}
	return practice.Question{}
}

func kind(err error) apperr.Kind {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Kind
	}
	return -1
}

func TestSubjectiveGradingQuotaAndPending(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	s := termSession(t, f, uid, sid)
	q := question(s, "意境")
	if q.ID == 0 {
		t.Fatalf("没有名词解释：%+v", s.Questions)
	}
	key := nextKey()
	g, err := f.pr.SubmitSubjective(ctx, uid, s.ID, practice.SubjectiveInput{QuestionID: q.ID, Key: key, Answer: "意境是情景交融、虚实相生的艺术境界。", Duration: 120})
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != "done" || g.Score == nil || *g.Score <= 0 || len(g.Points) == 0 || !g.QuotaCharged || g.Rubric.Source == "" {
		t.Fatalf("批改：%+v", g)
	}
	for _, p := range g.Points {
		if p.Verdict != "miss" && p.Quote == "" {
			t.Errorf("命中的采分点要引用考生原话：%+v", p)
		}
	}
	again, err := f.pr.SubmitSubjective(ctx, uid, s.ID, practice.SubjectiveInput{QuestionID: q.ID, Key: key, Answer: "意境", Duration: 1})
	if err != nil || again.ID != g.ID {
		t.Fatalf("幂等：%+v %v", again, err)
	}
	var score string
	_ = f.db.QueryRow("SELECT score FROM attempts WHERE id = ?", g.AttemptID).Scan(&score)
	if score == "" {
		t.Error("作答应记下得分")
	}

	// 空答案不提交、不扣次数。
	if _, err := f.pr.SubmitSubjective(ctx, uid, s.ID, practice.SubjectiveInput{QuestionID: q.ID, Key: nextKey(), Answer: "  "}); kind(err) != apperr.BadRequest {
		t.Errorf("空答案：%v", err)
	}
	// 免费版每天 3 次：再批 2 次后，第 4 次存为待批改，不调模型、不扣次数。
	for i := 0; i < 2; i++ {
		if g, err := f.pr.SubmitSubjective(ctx, uid, s.ID, practice.SubjectiveInput{QuestionID: q.ID, Key: nextKey(), Answer: "情景交融"}); err != nil || g.Status != "done" {
			t.Fatalf("第 %d 次：%+v %v", i+2, g, err)
		}
	}
	queued, err := f.pr.SubmitSubjective(ctx, uid, s.ID, practice.SubjectiveInput{QuestionID: q.ID, Key: nextKey(), Answer: "意境是情景交融的境界"})
	if err != nil || queued.Status != "queued_quota" || queued.Score != nil {
		t.Fatalf("次数用完：%+v %v", queued, err)
	}
	items, left, err := f.pr.Pending(ctx, uid)
	if err != nil || len(items) != 1 || left == nil || *left != 0 {
		t.Fatalf("待批改：%+v %v %v", items, left, err)
	}
	done, remain, err := f.pr.SubmitPending(ctx, uid)
	if err != nil || len(done) != 0 || remain != 1 {
		t.Fatalf("当天次数用完不能提交：%d %d %v", len(done), remain, err)
	}
	// 次日 0 点后一键提交。
	f.clock = f.clock.Add(24 * time.Hour)
	done, remain, err = f.pr.SubmitPending(ctx, uid)
	if err != nil || len(done) != 1 || remain != 0 || done[0].Status != "done" || done[0].Trigger != "pending_resubmit" {
		t.Fatalf("次日提交待批改：%+v %d %v", done, remain, err)
	}
	var used int
	_ = f.db.QueryRow("SELECT used FROM quota_counters WHERE owner_user_id = ? AND quota_type = 'grading' ORDER BY period_key DESC LIMIT 1", uid).Scan(&used)
	if used != 1 {
		t.Errorf("次日应扣 1 次：%d", used)
	}
}

func TestLossDiagnosisDisputeAndRubricRegrade(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	s := termSession(t, f, uid, sid)
	q := question(s, "典型")
	// 只写了一半：采分点遗漏，知识点 M < 60 → 失分归为「知识没掌握」，收录错题本。
	g, err := f.pr.SubmitSubjective(ctx, uid, s.ID, practice.SubjectiveInput{QuestionID: q.ID, Key: nextKey(), Answer: "典型是文学里的人物。"})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Loss) == 0 || g.Loss[0].Type != "knowledge" || g.Extra.WrongBook != "added" || len(g.Extra.KPs) == 0 {
		t.Fatalf("失分诊断：%+v", g)
	}
	var used int
	usedNow := func() int {
		_ = f.db.QueryRow("SELECT used FROM quota_counters WHERE owner_user_id = ? AND quota_type = 'grading'", uid).Scan(&used)
		return used
	}
	before := usedNow()

	// 采分点没改过不能按新采分点重批。
	if _, err := f.pr.RegradeAfterRubricChange(ctx, uid, g.ID); kind(err) != apperr.Conflict {
		t.Errorf("没改采分点：%v", err)
	}
	// 复核重批一次，不扣次数；授权后台查看 72 小时；第二次复核 409。
	r, err := f.pr.Dispute(ctx, uid, g.ID, practice.DisputeInput{Reason: "hit_missed", Note: "我写到了", AllowAccess: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Trigger != "dispute_recheck" || r.ParentID != g.ID || r.QuotaCharged || !r.Disputed {
		t.Errorf("复核重批：%+v", r)
	}
	if usedNow() != before {
		t.Error("复核不扣次数")
	}
	var grants int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM content_access_grants WHERE user_id = ? AND expires_at > ?", uid, f.clock).Scan(&grants)
	if grants != 1 {
		t.Errorf("授权记录：%d", grants)
	}
	if _, err := f.pr.Dispute(ctx, uid, g.ID, practice.DisputeInput{Reason: "other"}); kind(err) != apperr.Conflict {
		t.Errorf("重复复核：%v", err)
	}
	if _, err := f.pr.Dispute(ctx, uid, r.ID, practice.DisputeInput{Reason: "other"}); kind(err) != apperr.Conflict {
		t.Errorf("复核出来的批改不能再复核：%v", err)
	}

	// 改采分点（只留一个与答案相符的点）后按新采分点重批：拿满分，撤回这次收录的错题，历史批改不变。
	if _, err := f.db.Exec("DELETE FROM rubric_points WHERE question_id = ?", q.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("INSERT INTO rubric_points (owner_user_id, question_id, seq, content, score, origin) VALUES (?, ?, 1, '文学里的人物', 10, 'user_confirmed')", uid, q.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE questions SET rubric_version = rubric_version + 1, score = 10 WHERE id = ?", q.ID); err != nil {
		t.Fatal(err)
	}
	old, _ := f.pr.Grading(ctx, uid, g.ID)
	if !old.RubricChanged {
		t.Error("应提示采分点改过")
	}
	n, err := f.pr.RegradeAfterRubricChange(ctx, uid, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n.Score == nil || *n.Score != 10 || n.Rubric.Source != "user_confirmed" || n.QuotaCharged {
		t.Errorf("按新采分点重批：%+v", n)
	}
	wb, _ := f.pr.WrongBook(ctx, uid, sid)
	if wb.Total != 0 {
		t.Errorf("满分后撤回「没拿满分」错题：%+v", wb)
	}
	still, _ := f.pr.Grading(ctx, uid, g.ID)
	if *still.Score != *g.Score || len(still.Points) != len(g.Points) {
		t.Error("历史批改不应改变")
	}

	// 7.8 灰度对比：主观题批改按「模型 + 提示词版本」给出异议率（只有计数）。
	adm := admin.New(admin.Deps{DB: f.db, Params: params.New(dbq.New(f.db)), Now: func() time.Time { return f.clock }})
	ov, err := adm.AIOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, task := range ov.Tasks {
		for _, v := range task.Versions {
			if task.Capability == "grade_subjective" && v.Model == "mock" && v.DisputeRate != nil {
				found = *v.DisputeRate > 0 && *v.DisputeRate <= 1
			}
		}
	}
	if !found {
		t.Errorf("主观题批改版本应有异议率：%+v", ov.Tasks)
	}
}

func TestGradingOwnership(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	a, sa := f.user(t)
	b, _ := f.user(t)
	f.imported(t, a, sa)
	s := termSession(t, f, a, sa)
	q := question(s, "意境")
	g, err := f.pr.SubmitSubjective(ctx, a, s.ID, practice.SubjectiveInput{QuestionID: q.ID, Key: nextKey(), Answer: "情景交融"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.pr.SubmitSubjective(ctx, b, s.ID, practice.SubjectiveInput{QuestionID: q.ID, Key: nextKey(), Answer: "情景交融"})
	if kind(err) != apperr.NotFound {
		t.Errorf("别人的会话：%v", err)
	}
	if _, err := f.pr.Grading(ctx, b, g.ID); kind(err) != apperr.NotFound {
		t.Errorf("别人的批改：%v", err)
	}
	if _, err := f.pr.Dispute(ctx, b, g.ID, practice.DisputeInput{Reason: "other"}); kind(err) != apperr.NotFound {
		t.Errorf("别人的异议：%v", err)
	}
	if _, err := f.pr.RegradeAfterRubricChange(ctx, b, g.ID); kind(err) != apperr.NotFound {
		t.Errorf("别人的重批：%v", err)
	}
	if items, _, err := f.pr.Pending(ctx, b); err != nil || len(items) != 0 {
		t.Errorf("待批改只看自己的：%v %v", items, err)
	}
}

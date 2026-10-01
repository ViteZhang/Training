package profile_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/params"
	"peetraining-server/internal/profile"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
	"peetraining-server/internal/testenv"
)

type fx struct {
	svc   *profile.Service
	db    *sql.DB
	oss   *oss.Mock
	clock *time.Time
}

func setup(t *testing.T) *fx {
	t.Helper()
	env := testenv.New(t)
	ctx := context.Background()
	db, err := store.OpenMySQL(ctx, env.MySQLDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.MigrateUp(ctx, db, logx.New(io.Discard, slog.LevelInfo)); err != nil {
		t.Fatal(err)
	}
	// 北京时间 2026-10-02 10:00，距 2026-12-20 专业课考试 79 天 → 强化期
	now := time.Date(2026, 10, 2, 2, 0, 0, 0, time.UTC)
	f := &fx{db: db, oss: oss.NewMock(), clock: &now}
	f.svc = profile.New(db, params.New(dbq.New(db)), f.oss, func() time.Time { return *f.clock })
	return f
}

func (f *fx) user(t *testing.T, phone string) uint64 {
	t.Helper()
	res, err := f.db.Exec("INSERT INTO users (phone, invite_code) VALUES (?, ?)", phone, phone[3:])
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return uint64(id)
}

func kind(err error) apperr.Kind {
	if e, ok := apperr.As(err); ok {
		return e.Kind
	}
	return 0
}

func ptr[T any](v T) *T { return &v }

func TestSubjectsLimitAndOwnership(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid := f.user(t, "13800000001")
	other := f.user(t, "13800000002")

	for i, name := range []string{"中国语言文学基础", "文学评论写作", "中国古代文学史"} {
		s, err := f.svc.CreateSubject(ctx, uid, profile.SubjectInput{Name: name, FullScore: 150, TargetScore: ptr(105)})
		if err != nil {
			t.Fatalf("第 %d 门：%v", i+1, err)
		}
		if s.BankID == 0 || s.Name != name || !s.TargetScore.Valid {
			t.Fatalf("专业课与题库：%+v", s)
		}
	}
	list, _ := f.svc.Subjects(ctx, uid)
	if len(list.Items) != 3 || list.MaxSubjects != 3 || list.CanAdd {
		t.Fatalf("免费版 3 门：%+v", list)
	}
	_, err := f.svc.CreateSubject(ctx, uid, profile.SubjectInput{Name: "第四门", FullScore: 150})
	if e, _ := apperr.As(err); e == nil || e.Kind != apperr.QuotaExceeded || e.Detail["reason"] != "subject_limit" {
		t.Fatalf("免费版第 4 门应提示开会员：%v", err)
	}

	// 开通会员后可以加第 4 门，第 5 门不行。
	now := *f.clock
	if _, err := f.db.Exec("INSERT INTO memberships (owner_user_id, tier, source, starts_at, ends_at) VALUES (?, 'monthly', 'redeem', ?, ?)", uid, now.Add(-time.Hour), now.Add(30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateSubject(ctx, uid, profile.SubjectInput{Name: "第四门", FullScore: 300}); err != nil {
		t.Fatalf("会员第 4 门：%v", err)
	}
	if _, err := f.svc.CreateSubject(ctx, uid, profile.SubjectInput{Name: "第五门", FullScore: 150}); kind(err) != apperr.BadRequest {
		t.Fatalf("最多 4 门：%v", err)
	}

	// 目标分不能超过满分；可以清除目标。
	s := list.Items[0]
	if _, err := f.svc.CreateSubject(ctx, other, profile.SubjectInput{Name: "x", FullScore: 100, TargetScore: ptr(120)}); kind(err) != apperr.BadRequest {
		t.Fatal("目标分超过满分应 400")
	}
	upd, err := f.svc.UpdateSubject(ctx, uid, s.ID, profile.SubjectPatch{ClearTarget: true, Name: ptr("改名"), IsEssay: ptr(true)})
	if err != nil || upd.TargetScore.Valid || upd.Name != "改名" || !upd.IsEssay {
		t.Fatalf("修改：%+v %v", upd, err)
	}
	var setBy sql.NullString
	_ = f.db.QueryRow("SELECT essay_set_by FROM subjects WHERE id = ?", s.ID).Scan(&setBy)
	if setBy.String != "user" {
		t.Error("用户纠正作文课判断后记为 user")
	}

	// 拿别人的 ID：404。
	if _, err := f.svc.UpdateSubject(ctx, other, s.ID, profile.SubjectPatch{Name: ptr("x")}); kind(err) != apperr.NotFound {
		t.Fatal("改别人的专业课应 404")
	}
	if err := f.svc.DeleteSubject(ctx, other, s.ID); kind(err) != apperr.NotFound {
		t.Fatal("删别人的专业课应 404")
	}
}

func TestDeleteSubjectCascades(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid := f.user(t, "13800000001")
	s, err := f.svc.CreateSubject(ctx, uid, profile.SubjectInput{Name: "中国文学", FullScore: 150})
	if err != nil {
		t.Fatal(err)
	}
	keep, _ := f.svc.CreateSubject(ctx, uid, profile.SubjectInput{Name: "保留", FullScore: 150})
	if _, err := f.db.Exec("INSERT INTO materials (owner_user_id, bank_id, file_name, format, sha256, right_confirmed_at) VALUES (?, ?, 'a.pdf', 'pdf', REPEAT('a',64), NOW(3))", uid, s.BankID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("INSERT INTO paper_sessions (owner_user_id, subject_id, paper_kind, paper_title, mode, full_score) VALUES (?, ?, 'real_exam', 't', 'mock', 150)", uid, s.ID); err != nil {
		t.Fatal(err)
	}
	f.oss.Put(oss.BankPrefix(uid, s.BankID)+"m/1.pdf", []byte("x"))
	f.oss.Put(oss.BankPrefix(uid, keep.BankID)+"m/2.pdf", []byte("y"))

	if err := f.svc.DeleteSubject(ctx, uid, s.ID); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"SELECT COUNT(*) FROM banks WHERE id = ?", "SELECT COUNT(*) FROM materials WHERE bank_id = ?"} {
		var n int
		_ = f.db.QueryRow(q, s.BankID).Scan(&n)
		if n != 0 {
			t.Errorf("%s 还有 %d 行", q, n)
		}
	}
	var sessions int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM paper_sessions WHERE subject_id = ?", s.ID).Scan(&sessions)
	if sessions != 0 {
		t.Error("学习记录应一起删除")
	}
	if keys := f.oss.Keys(); len(keys) != 1 || keys[0] != oss.BankPrefix(uid, keep.BankID)+"m/2.pdf" {
		t.Errorf("OSS：%v", keys)
	}
}

func TestProfilePendingChanges(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid := f.user(t, "13800000001")

	if _, err := f.svc.Get(ctx, uid); kind(err) != apperr.NotFound {
		t.Fatal("还没建档案应 404")
	}
	years, err := f.svc.ExamYears(ctx)
	if err != nil || len(years) != 1 || years[0].ExamYear != 2027 {
		t.Fatalf("考试年份：%+v %v", years, err)
	}

	// 第一次创建立即生效；系统建议强化期，选强化期不算手动。
	p, err := f.svc.Upsert(ctx, uid, profile.Input{ExamYear: 2027, Stage: rules.Strengthen, DailyMinutes: 45, ReminderTimes: []string{"07:30"}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Stage != "strengthen" || p.StageManual || p.DailyMinutes != 45 || p.Suggested != rules.Strengthen || p.DaysToExam != 79 {
		t.Fatalf("创建：%+v", p)
	}

	// 改阶段和时长：从明天起生效，目标院校立即生效。
	p, err = f.svc.Upsert(ctx, uid, profile.Input{ExamYear: 2027, Stage: rules.Sprint, DailyMinutes: 60, TargetSchoolMajor: ptr("海南大学 · 中国语言文学")})
	if err != nil {
		t.Fatal(err)
	}
	if p.Stage != "strengthen" || p.DailyMinutes != 45 || !p.PendingStage.Valid || !p.PendingEffectiveOn.Valid || !p.TargetSchoolMajor.Valid {
		t.Fatalf("待生效：%+v", p)
	}
	if got := rules.DayFromDateColumn(p.PendingEffectiveOn.Time).String(); got != "2026-10-03" {
		t.Fatalf("生效日应是北京时间明天：%s", got)
	}
	// 当天再读还是旧值；过了北京时间 0 点生效，且记为手动选择。
	*f.clock = time.Date(2026, 10, 2, 15, 59, 0, 0, time.UTC)
	if p, _ = f.svc.Get(ctx, uid); p.Stage != "strengthen" {
		t.Fatal("当天不生效")
	}
	*f.clock = time.Date(2026, 10, 2, 16, 1, 0, 0, time.UTC)
	p, _ = f.svc.Get(ctx, uid)
	if p.Stage != "sprint" || p.DailyMinutes != 60 || p.PendingStage.Valid || p.PendingEffectiveOn.Valid || !p.StageManual {
		t.Fatalf("次日生效：%+v", p)
	}

	// 改了又改回去：取消待生效。
	_, _ = f.svc.Upsert(ctx, uid, profile.Input{ExamYear: 2027, Stage: rules.Final, DailyMinutes: 60})
	p, _ = f.svc.Upsert(ctx, uid, profile.Input{ExamYear: 2027, Stage: rules.Sprint, DailyMinutes: 60})
	if p.PendingStage.Valid || p.PendingEffectiveOn.Valid {
		t.Fatalf("改回原值应取消待生效：%+v", p)
	}
	if _, err := f.svc.Upsert(ctx, uid, profile.Input{ExamYear: 2031, Stage: rules.Sprint, DailyMinutes: 60}); kind(err) != apperr.BadRequest {
		t.Fatal("没有配置的年份应 400")
	}
}

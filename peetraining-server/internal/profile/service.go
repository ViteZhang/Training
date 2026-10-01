// Package profile 是备考档案与专业课（T07）：考试年份、专业课与目标分、备考阶段、每日时长、提醒、目标院校。
package profile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/membership"
	"peetraining-server/internal/params"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

// Service 是备考档案服务。
type Service struct {
	db     *sql.DB
	q      *dbq.Queries
	params *params.Store
	oss    oss.Store
	now    func() time.Time
}

func New(db *sql.DB, ps *params.Store, store oss.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, q: dbq.New(db), params: ps, oss: store, now: now}
}

func (s *Service) today() rules.Day { return rules.DayOf(s.now()) }

// ExamYears 返回还没考完的考试年份（1.1）。
func (s *Service) ExamYears(ctx context.Context) ([]dbq.ExamDate, error) {
	return s.q.ListExamDatesFrom(ctx, s.today().Date())
}

// Profile 是备考档案的完整视图。
type Profile struct {
	dbq.StudyProfile
	DaysToExam      int
	SubjectExamDate time.Time
	Suggested       rules.Stage
	SuggestedReason string
}

var stageNames = map[rules.Stage]string{rules.Foundation: "基础期", rules.Strengthen: "强化期", rules.Sprint: "冲刺期", rules.Final: "考前期"}

// Get 返回备考档案；还没建档案时返回 404（App 进入 1.1）。
// 改阶段或时长「从明天起生效」：读取时到了生效日就把待生效的修改落地。
func (s *Service) Get(ctx context.Context, userID uint64) (Profile, error) {
	p, err := s.q.GetStudyProfile(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, apperr.NotFoundErr()
	}
	if err != nil {
		return Profile{}, err
	}
	if p.PendingEffectiveOn.Valid && rules.DayFromDateColumn(p.PendingEffectiveOn.Time) <= s.today() {
		applyPending(&p)
		if err := s.q.UpdateStudyProfile(ctx, updateParams(p)); err != nil {
			return Profile{}, err
		}
	}
	return s.view(ctx, p)
}

func applyPending(p *dbq.StudyProfile) {
	if p.PendingStage.Valid {
		p.Stage = dbq.StudyProfilesStage(p.PendingStage.StudyProfilesPendingStage)
	}
	if p.PendingDailyMinutes.Valid {
		p.DailyMinutes = uint16(p.PendingDailyMinutes.Int16)
	}
	p.PendingStage = dbq.NullStudyProfilesPendingStage{}
	p.PendingDailyMinutes = sql.NullInt16{}
	p.PendingEffectiveOn = sql.NullTime{}
}

func (s *Service) view(ctx context.Context, p dbq.StudyProfile) (Profile, error) {
	ed, err := s.q.GetExamDate(ctx, p.ExamYear)
	if err != nil {
		return Profile{}, fmt.Errorf("读取 %d 年初试日期：%w", p.ExamYear, err)
	}
	rp, err := s.params.Rules(ctx)
	if err != nil {
		return Profile{}, err
	}
	days := int(rules.DayFromDateColumn(ed.SubjectExamDate) - s.today())
	if days < 0 {
		days = 0
	}
	sug := rules.DefaultStage(days, rp.Stage)
	return Profile{
		StudyProfile: p, DaysToExam: days, SubjectExamDate: ed.SubjectExamDate, Suggested: sug,
		SuggestedReason: fmt.Sprintf("距初试 %d 天，建议%s", days, stageNames[sug]),
	}, nil
}

// Input 是创建或修改档案的输入。
type Input struct {
	ExamYear          int
	Stage             rules.Stage
	DailyMinutes      int
	ReminderTimes     []string
	TargetSchoolMajor *string // nil 表示不改；空串表示清除
	EssayWeeklyGoal   *int
	MockTimeReminders *bool
	NotifyDaily       *bool
	NotifyReviewDue   *bool
	NotifyTaskDone    *bool
}

// Upsert 创建或修改备考档案（1.3、6.9）。
// 第一次创建立即生效；之后改阶段或每日时长从明天起生效，其余字段立即生效（PRD 6.9）。
// 用户选的阶段与系统按日期建议的不同，记为手动选择，之后系统只提示、不自动覆盖（PRD 11.4）。
func (s *Service) Upsert(ctx context.Context, userID uint64, in Input) (Profile, error) {
	ed, err := s.q.GetExamDate(ctx, uint16(in.ExamYear))
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, apperr.New(apperr.BadRequest, "考试年份不正确")
	}
	if err != nil {
		return Profile{}, err
	}
	rp, err := s.params.Rules(ctx)
	if err != nil {
		return Profile{}, err
	}
	days := int(rules.DayFromDateColumn(ed.SubjectExamDate) - s.today())
	manual := in.Stage != rules.DefaultStage(max(days, 0), rp.Stage)

	cur, err := s.q.GetStudyProfile(ctx, userID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		reminders, _ := json.Marshal(nonNil(in.ReminderTimes))
		err = s.q.InsertStudyProfile(ctx, dbq.InsertStudyProfileParams{
			UserID: userID, ExamYear: uint16(in.ExamYear), Stage: dbq.StudyProfilesStage(in.Stage), StageManual: manual,
			DailyMinutes: uint16(in.DailyMinutes), ReminderTimes: reminders, TargetSchoolMajor: nullStr(in.TargetSchoolMajor),
			EssayWeeklyGoal: uint8(deref(in.EssayWeeklyGoal, 2)), MockTimeReminders: derefB(in.MockTimeReminders, true),
			NotifyDaily: derefB(in.NotifyDaily, true), NotifyReviewDue: derefB(in.NotifyReviewDue, true), NotifyTaskDone: derefB(in.NotifyTaskDone, true),
		})
		if err != nil {
			return Profile{}, err
		}
		return s.Get(ctx, userID)
	case err != nil:
		return Profile{}, err
	}

	tomorrow := sql.NullTime{Time: s.today().AddDays(1).Date(), Valid: true}
	p := cur
	p.ExamYear = uint16(in.ExamYear)
	if dbq.StudyProfilesStage(in.Stage) != cur.Stage {
		p.PendingStage = dbq.NullStudyProfilesPendingStage{StudyProfilesPendingStage: dbq.StudyProfilesPendingStage(in.Stage), Valid: true}
		p.PendingEffectiveOn = tomorrow
		p.StageManual = manual
	} else if p.PendingStage.Valid {
		// 改回原阶段：取消待生效的修改。
		p.PendingStage = dbq.NullStudyProfilesPendingStage{}
	}
	if uint16(in.DailyMinutes) != cur.DailyMinutes {
		p.PendingDailyMinutes = sql.NullInt16{Int16: int16(in.DailyMinutes), Valid: true}
		p.PendingEffectiveOn = tomorrow
	} else if p.PendingDailyMinutes.Valid {
		p.PendingDailyMinutes = sql.NullInt16{}
	}
	if !p.PendingStage.Valid && !p.PendingDailyMinutes.Valid {
		p.PendingEffectiveOn = sql.NullTime{}
	}
	if in.ReminderTimes != nil {
		p.ReminderTimes, _ = json.Marshal(in.ReminderTimes)
	}
	if in.TargetSchoolMajor != nil {
		p.TargetSchoolMajor = nullStr(in.TargetSchoolMajor)
	}
	if in.EssayWeeklyGoal != nil {
		p.EssayWeeklyGoal = uint8(*in.EssayWeeklyGoal)
	}
	setB(&p.MockTimeReminders, in.MockTimeReminders)
	setB(&p.NotifyDaily, in.NotifyDaily)
	setB(&p.NotifyReviewDue, in.NotifyReviewDue)
	setB(&p.NotifyTaskDone, in.NotifyTaskDone)
	if err := s.q.UpdateStudyProfile(ctx, updateParams(p)); err != nil {
		return Profile{}, err
	}
	return s.Get(ctx, userID)
}

func updateParams(p dbq.StudyProfile) dbq.UpdateStudyProfileParams {
	return dbq.UpdateStudyProfileParams{
		ExamYear: p.ExamYear, Stage: p.Stage, StageManual: p.StageManual, DailyMinutes: p.DailyMinutes,
		PendingStage: p.PendingStage, PendingDailyMinutes: p.PendingDailyMinutes, PendingEffectiveOn: p.PendingEffectiveOn,
		ReminderTimes: p.ReminderTimes, TargetSchoolMajor: p.TargetSchoolMajor, EssayWeeklyGoal: p.EssayWeeklyGoal,
		MockTimeReminders: p.MockTimeReminders, NotifyDaily: p.NotifyDaily, NotifyReviewDue: p.NotifyReviewDue,
		NotifyTaskDone: p.NotifyTaskDone, UserID: p.UserID,
	}
}

// Subject 是专业课及其题库统计。
type Subject = dbq.ListSubjectsRow

// SubjectList 是专业课列表与可添加的上限。
type SubjectList struct {
	Items       []Subject
	MaxSubjects int
	CanAdd      bool
}

type quotaLimits struct {
	Free   struct{ Subjects int } `json:"free"`
	Member struct{ Subjects int } `json:"member"`
}

// maxSubjects 返回当前身份最多几门专业课：免费版 3 门，会员 4 门（D12）。
func (s *Service) maxSubjects(ctx context.Context, userID uint64) (int, bool, error) {
	var q quotaLimits
	if err := s.params.Get(ctx, "quota", &q); err != nil {
		return 0, false, err
	}
	member, err := membership.IsMember(ctx, s.q, userID, s.now().UTC())
	if err != nil {
		return 0, false, err
	}
	if member {
		return q.Member.Subjects, true, nil
	}
	return q.Free.Subjects, false, nil
}

// Subjects 返回我的专业课（含题库统计）与是否还能添加。
func (s *Service) Subjects(ctx context.Context, userID uint64) (SubjectList, error) {
	items, err := s.q.ListSubjects(ctx, userID)
	if err != nil {
		return SubjectList{}, err
	}
	limit, _, err := s.maxSubjects(ctx, userID)
	if err != nil {
		return SubjectList{}, err
	}
	return SubjectList{Items: items, MaxSubjects: limit, CanAdd: len(items) < limit}, nil
}

// SubjectInput 是新建专业课的输入。
type SubjectInput struct {
	Name        string
	Code        *string
	FullScore   int
	TargetScore *int
}

// CreateSubject 添加专业课并建好它的自建题库（一门课一个题库）。
// 最多 4 门；免费版最多 3 门，第 4 门返回额度不足，App 引导开通会员（D12）。
func (s *Service) CreateSubject(ctx context.Context, userID uint64, in SubjectInput) (Subject, error) {
	if err := validateTarget(in.TargetScore, in.FullScore); err != nil {
		return Subject{}, err
	}
	limit, member, err := s.maxSubjects(ctx, userID)
	if err != nil {
		return Subject{}, err
	}
	var id uint64
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		n, err := q.CountSubjects(ctx, userID)
		if err != nil {
			return err
		}
		if int(n) >= limit {
			if !member {
				return apperr.New(apperr.QuotaExceeded, "免费版最多添加 3 门专业课，开通会员后可以添加第 4 门").
					With("quota_type", "subjects").With("reason", "subject_limit").With("limit", limit)
			}
			return apperr.New(apperr.BadRequest, fmt.Sprintf("最多添加 %d 门专业课", limit)).With("reason", "subject_limit")
		}
		name := strings.TrimSpace(in.Name)
		res, err := q.CreateSubject(ctx, dbq.CreateSubjectParams{
			OwnerUserID: userID, Name: name, Code: nullStr(in.Code), FullScore: uint16(in.FullScore),
			TargetScore: nullInt16(in.TargetScore), SortOrder: uint8(n),
		})
		if err != nil {
			return err
		}
		id = uint64(res)
		_, err = q.CreateUserBank(ctx, dbq.CreateUserBankParams{
			OwnerUserID: sql.NullInt64{Int64: int64(userID), Valid: true}, SubjectID: sql.NullInt64{Int64: int64(id), Valid: true},
			Title: name, SubjectCode: nullStr(in.Code),
		})
		return err
	})
	if err != nil {
		return Subject{}, err
	}
	return s.subject(ctx, userID, id)
}

func (s *Service) subject(ctx context.Context, userID, id uint64) (Subject, error) {
	items, err := s.q.ListSubjects(ctx, userID)
	if err != nil {
		return Subject{}, err
	}
	for _, it := range items {
		if it.ID == id {
			return it, nil
		}
	}
	return Subject{}, apperr.NotFoundErr()
}

// SubjectPatch 是修改专业课的输入；nil 表示不改。ClearCode / ClearTarget 表示清除。
type SubjectPatch struct {
	Name        *string
	Code        *string
	ClearCode   bool
	FullScore   *int
	TargetScore *int
	ClearTarget bool
	IsEssay     *bool
}

// UpdateSubject 修改专业课；改目标分立即生效（2.1f）；用户纠正作文课判断后不再自动改（D13）。
func (s *Service) UpdateSubject(ctx context.Context, userID, id uint64, in SubjectPatch) (Subject, error) {
	cur, err := s.q.GetSubject(ctx, dbq.GetSubjectParams{ID: id, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return Subject{}, apperr.NotFoundErr()
	}
	if err != nil {
		return Subject{}, err
	}
	p := dbq.UpdateSubjectParams{
		Name: cur.Name, Code: cur.Code, FullScore: cur.FullScore, TargetScore: cur.TargetScore,
		IsEssay: cur.IsEssay, EssaySetBy: cur.EssaySetBy, ID: id, OwnerUserID: userID,
	}
	if in.Name != nil {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if in.ClearCode {
		p.Code = sql.NullString{}
	} else if in.Code != nil {
		p.Code = nullStr(in.Code)
	}
	if in.FullScore != nil {
		p.FullScore = uint16(*in.FullScore)
	}
	if in.ClearTarget {
		p.TargetScore = sql.NullInt16{}
	} else if in.TargetScore != nil {
		p.TargetScore = nullInt16(in.TargetScore)
	}
	if p.TargetScore.Valid {
		t := int(p.TargetScore.Int16)
		if err := validateTarget(&t, int(p.FullScore)); err != nil {
			return Subject{}, err
		}
	}
	if in.IsEssay != nil {
		p.IsEssay = *in.IsEssay
		p.EssaySetBy = dbq.NullSubjectsEssaySetBy{SubjectsEssaySetBy: dbq.SubjectsEssaySetByUser, Valid: true}
	}
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if err := q.UpdateSubject(ctx, p); err != nil {
			return err
		}
		return q.UpdateBankForSubject(ctx, dbq.UpdateBankForSubjectParams{
			Title: p.Name, SubjectCode: p.Code, SubjectID: sql.NullInt64{Int64: int64(id), Valid: true},
			OwnerUserID: sql.NullInt64{Int64: int64(userID), Valid: true},
		})
	})
	if err != nil {
		return Subject{}, err
	}
	return s.subject(ctx, userID, id)
}

// DeleteSubject 删除专业课：连同它的题库、资料（含 OSS 原件）和学习记录（PRD 11.12）。
func (s *Service) DeleteSubject(ctx context.Context, userID, id uint64) error {
	cur, err := s.q.GetSubject(ctx, dbq.GetSubjectParams{ID: id, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	}
	if err != nil {
		return err
	}
	// 先删 OSS 原件：失败时数据库还在，用户可以重试；反过来会留下找不到归属的文件。
	if _, err := s.oss.DeletePrefix(ctx, oss.BankPrefix(userID, cur.BankID)); err != nil {
		return err
	}
	sid := sql.NullInt64{Int64: int64(id), Valid: true}
	return store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if err := q.DeleteSubjectSessions(ctx, dbq.DeleteSubjectSessionsParams{OwnerUserID: userID, SubjectID: id}); err != nil {
			return err
		}
		if err := q.DeleteSubjectPracticeSessions(ctx, dbq.DeleteSubjectPracticeSessionsParams{OwnerUserID: userID, SubjectID: sid}); err != nil {
			return err
		}
		if err := q.DeleteSubjectExports(ctx, dbq.DeleteSubjectExportsParams{OwnerUserID: userID, SubjectID: id}); err != nil {
			return err
		}
		n, err := q.DeleteSubject(ctx, dbq.DeleteSubjectParams{ID: id, OwnerUserID: userID})
		if err == nil && n == 0 {
			return apperr.NotFoundErr()
		}
		return err
	})
}

func validateTarget(target *int, full int) error {
	if target != nil && (*target < 0 || *target > full) {
		return apperr.New(apperr.BadRequest, "目标分不能超过满分")
	}
	return nil
}

func nullStr(v *string) sql.NullString {
	if v == nil || strings.TrimSpace(*v) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: strings.TrimSpace(*v), Valid: true}
}

func nullInt16(v *int) sql.NullInt16 {
	if v == nil {
		return sql.NullInt16{}
	}
	return sql.NullInt16{Int16: int16(*v), Valid: true}
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func deref(v *int, def int) int {
	if v == nil {
		return def
	}
	return *v
}

func derefB(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}

func setB(dst *bool, v *bool) {
	if v != nil {
		*dst = *v
	}
}

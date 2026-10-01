package http

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"peetraining-server/internal/gen"
	"peetraining-server/internal/profile"
	"peetraining-server/internal/rules"
)

// 1.1–1.3 专业课与备考档案、6.9 备考设置（T07）。

func (h *Handlers) ListExamYears(c *gin.Context) {
	list, err := h.deps.Profile.ExamYears(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.ExamYear, len(list))
	for i, e := range list {
		items[i] = gen.ExamYear{
			ExamYear: int(e.ExamYear), Label: e.Label,
			FirstExamStart: openapi_types.Date{Time: e.FirstExamStart}, FirstExamEnd: openapi_types.Date{Time: e.FirstExamEnd},
			SubjectExamDate: openapi_types.Date{Time: e.SubjectExamDate},
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *Handlers) GetProfile(c *gin.Context) {
	p, err := h.deps.Profile.Get(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toProfile(p))
}

func (h *Handlers) UpsertProfile(c *gin.Context) {
	var req gen.StudyProfileInput
	if !bind(c, &req) {
		return
	}
	in := profile.Input{
		ExamYear: req.ExamYear, Stage: rules.Stage(req.Stage), DailyMinutes: int(req.DailyMinutes),
		EssayWeeklyGoal: req.EssayWeeklyGoal, MockTimeReminders: req.MockTimeReminders,
		NotifyDaily: req.NotifyDaily, NotifyReviewDue: req.NotifyReviewDue, NotifyTaskDone: req.NotifyTaskDone,
	}
	if req.ReminderTimes != nil {
		in.ReminderTimes = *req.ReminderTimes
	}
	if req.TargetSchoolMajor.IsSpecified() {
		v := ""
		if !req.TargetSchoolMajor.IsNull() {
			v = req.TargetSchoolMajor.MustGet()
		}
		in.TargetSchoolMajor = &v
	}
	p, err := h.deps.Profile.Upsert(c.Request.Context(), currentUser(c), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toProfile(p))
}

func (h *Handlers) ListSubjects(c *gin.Context) {
	l, err := h.deps.Profile.Subjects(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.Subject, len(l.Items))
	for i, s := range l.Items {
		items[i] = toSubject(s)
	}
	c.JSON(http.StatusOK, gen.SubjectList{Items: items, MaxSubjects: l.MaxSubjects, CanAdd: l.CanAdd})
}

func (h *Handlers) CreateSubject(c *gin.Context, _ gen.CreateSubjectParams) {
	var req gen.SubjectInput
	if !bind(c, &req) {
		return
	}
	s, err := h.deps.Profile.CreateSubject(c.Request.Context(), currentUser(c), profile.SubjectInput{
		Name: req.Name, Code: nullableStr(req.Code), FullScore: int(req.FullScore), TargetScore: nullableInt(req.TargetScore),
	})
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, toSubject(s))
}

func (h *Handlers) UpdateSubject(c *gin.Context, subjectID int64) {
	var req gen.SubjectPatch
	if !bind(c, &req) {
		return
	}
	in := profile.SubjectPatch{Name: req.Name, IsEssay: req.IsEssay}
	if req.FullScore != nil {
		v := int(*req.FullScore)
		in.FullScore = &v
	}
	if req.Code.IsSpecified() {
		in.ClearCode = req.Code.IsNull()
		in.Code = nullableStr(req.Code)
	}
	if req.TargetScore.IsSpecified() {
		in.ClearTarget = req.TargetScore.IsNull()
		in.TargetScore = nullableInt(req.TargetScore)
	}
	s, err := h.deps.Profile.UpdateSubject(c.Request.Context(), currentUser(c), uint64(subjectID), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toSubject(s))
}

func (h *Handlers) DeleteSubject(c *gin.Context, subjectID int64) {
	if err := h.deps.Profile.DeleteSubject(c.Request.Context(), currentUser(c), uint64(subjectID)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func toSubject(s profile.Subject) gen.Subject {
	out := gen.Subject{
		Id: int64(s.ID), Name: s.Name, FullScore: int(s.FullScore), IsEssay: s.IsEssay, BankId: int64(s.BankID),
		QuestionCount: int(s.QuestionCount), KpCount: int(s.KpCount), MaterialCount: int(s.MaterialCount),
	}
	if s.Code.Valid {
		out.Code = &s.Code.String
	}
	if s.TargetScore.Valid {
		t := int(s.TargetScore.Int16)
		out.TargetScore = &t
	}
	return out
}

func toProfile(p profile.Profile) gen.StudyProfile {
	out := gen.StudyProfile{
		ExamYear: int(p.ExamYear), Stage: gen.Stage(p.Stage), StageManual: p.StageManual,
		SuggestedStage: gen.Stage(p.Suggested), SuggestedReason: &p.SuggestedReason, DaysToExam: p.DaysToExam,
		SubjectExamDate: openapi_types.Date{Time: p.SubjectExamDate}, DailyMinutes: int(p.DailyMinutes),
		ReminderTimes: []string{}, EssayWeeklyGoal: int(p.EssayWeeklyGoal), MockTimeReminders: p.MockTimeReminders,
		NotifyDaily: p.NotifyDaily, NotifyReviewDue: p.NotifyReviewDue, NotifyTaskDone: p.NotifyTaskDone,
	}
	_ = json.Unmarshal(p.ReminderTimes, &out.ReminderTimes)
	if p.PendingStage.Valid {
		st := gen.Stage(p.PendingStage.StudyProfilesPendingStage)
		out.PendingStage = &st
	}
	if p.PendingDailyMinutes.Valid {
		m := int(p.PendingDailyMinutes.Int16)
		out.PendingDailyMinutes = &m
	}
	if p.PendingEffectiveOn.Valid {
		out.PendingEffectiveOn = &openapi_types.Date{Time: p.PendingEffectiveOn.Time}
	}
	if p.TargetSchoolMajor.Valid {
		out.TargetSchoolMajor = &p.TargetSchoolMajor.String
	}
	return out
}

func nullableStr(n nullable.Nullable[string]) *string {
	if !n.IsSpecified() || n.IsNull() {
		return nil
	}
	v := n.MustGet()
	return &v
}

func nullableInt(n nullable.Nullable[int]) *int {
	if !n.IsSpecified() || n.IsNull() {
		return nil
	}
	v := n.MustGet()
	return &v
}

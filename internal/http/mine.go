package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/export"
	"peetraining-server/internal/feedback"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/score"
)

// 模块 6 我的（T24）：6.1 我的、6.4 导出题库、6.7 兑换码、6.13 意见反馈、6.14 考后回访。

func (h *Handlers) GetMeOverview(c *gin.Context) {
	o, err := h.deps.Profile.Overview(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.MeOverview{Materials: o.Materials, Questions: o.Questions, Wrong: o.Wrong, Essays: o.Essays})
}

func (h *Handlers) RedeemCode(c *gin.Context) {
	var body gen.RedeemCodeJSONBody
	if !bind(c, &body) {
		return
	}
	ctx := c.Request.Context()
	r, err := h.deps.Membership.Redeem(ctx, currentUser(c), body.Code)
	if err != nil {
		_ = c.Error(err)
		return
	}
	me, err := h.deps.Auth.GetMe(ctx, currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.RedeemResult{Tier: gen.RedeemResultTier(r.Tier), Days: r.Days, StartsAt: r.StartsAt, EndsAt: r.EndsAt, Membership: gen.MembershipStatus{IsMember: me.Membership.IsMember}}
	if me.Membership.IsMember {
		out.Membership.Tier = ptr(gen.MembershipStatusTier(me.Membership.Tier))
		out.Membership.EndsAt = ptr(me.Membership.EndsAt)
	}
	c.JSON(http.StatusOK, out)
}

func toGenSurvey(sv score.Survey) gen.Survey {
	out := gen.Survey{Open: sv.Open, ExamYear: sv.ExamYear, Submitted: sv.Submitted, RewardDays: sv.RewardDays, ShareConsent: sv.ShareConsent,
		Subjects: make([]gen.SurveySubject, len(sv.Subjects))}
	if sv.Retest != "" {
		out.RetestResult = ptr(gen.RetestResult(sv.Retest))
	}
	if sv.Admission != "" {
		out.Admission = ptr(gen.Admission(sv.Admission))
	}
	for i, s := range sv.Subjects {
		out.Subjects[i] = gen.SurveySubject{SubjectId: int64(s.SubjectID), Name: s.Name, FullScore: s.FullScore, Low: s.Low, High: s.High, Actual: f32p(s.Actual)}
	}
	return out
}

func (h *Handlers) GetSurvey(c *gin.Context) {
	sv, err := h.deps.Score.Survey(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenSurvey(sv))
}

func (h *Handlers) SubmitSurvey(c *gin.Context) {
	var body gen.SubmitSurveyJSONBody
	if !bind(c, &body) {
		return
	}
	in := score.SurveyInput{Scores: map[uint64]float64{}, Retest: string(body.RetestResult)}
	for _, s := range body.Scores {
		in.Scores[uint64(s.SubjectId)] = float64(s.Score)
	}
	if body.Admission != nil {
		in.Admission = string(*body.Admission)
	}
	if body.ShareConsent != nil {
		in.ShareConsent = *body.ShareConsent
	}
	sv, err := h.deps.Score.SubmitSurvey(c.Request.Context(), currentUser(c), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenSurvey(sv))
}

func toGenFeedback(f feedback.Feedback) gen.Feedback {
	return gen.Feedback{Id: int64(f.ID), Ftype: gen.FeedbackType(f.Type), Content: f.Content, ScreenshotKeys: f.Screenshots, AllowAccess: f.AllowAccess,
		Status: gen.FeedbackStatus(f.Status), Reply: optStr(f.Reply), RepliedAt: f.RepliedAt, CreatedAt: f.CreatedAt}
}

func (h *Handlers) SubmitFeedback(c *gin.Context) {
	var body gen.SubmitFeedbackJSONBody
	if !bind(c, &body) {
		return
	}
	in := feedback.Input{Type: string(body.Ftype), Content: body.Content, MaterialID: deref64(body.RelatedMaterialId)}
	if body.ScreenshotKeys != nil {
		in.Screenshots = *body.ScreenshotKeys
	}
	if body.AllowAccess != nil {
		in.AllowAccess = *body.AllowAccess
	}
	f, err := h.deps.Feedback.Submit(c.Request.Context(), currentUser(c), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, toGenFeedback(f))
}

func (h *Handlers) ListFeedbacks(c *gin.Context) {
	fs, err := h.deps.Feedback.List(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.Feedback, len(fs))
	for i, f := range fs {
		items[i] = toGenFeedback(f)
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func exportItem(i export.Item) gen.ExportItem { return gen.ExportItem{Count: i.Count, Pages: i.Pages} }

func (h *Handlers) GetExportPreview(c *gin.Context, subjectID gen.SubjectId) {
	p, err := h.deps.Export.Preview(c.Request.Context(), currentUser(c), uint64(subjectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.ExportPreview{SubjectId: int64(p.SubjectID), Questions: exportItem(p.Questions), Kps: exportItem(p.KPs), Wrong: exportItem(p.Wrong),
		AiVariants: exportItem(p.AIVariants)})
}

func toGenExport(j export.Job) gen.ExportJob {
	return gen.ExportJob{Id: int64(j.ID), SubjectId: int64(j.SubjectID), Format: gen.ExportJobFormat(j.Format), Status: gen.ExportJobStatus(j.Status), Pages: j.Pages,
		FileName: j.FileName, DownloadUrl: optStr(j.DownloadURL), ExpiresAt: j.ExpiresAt, CreatedAt: j.CreatedAt,
		Options: gen.ExportOptions{Questions: j.Options.Questions, Kps: j.Options.KPs, Wrong: j.Options.Wrong, AiVariants: j.Options.AIVariants}}
}

func (h *Handlers) CreateExport(c *gin.Context) {
	var body gen.CreateExportJSONBody
	if !bind(c, &body) {
		return
	}
	o := export.Options{Questions: body.Options.Questions, KPs: body.Options.Kps, Wrong: body.Options.Wrong, AIVariants: body.Options.AiVariants}
	j, err := h.deps.Export.Create(c.Request.Context(), currentUser(c), uint64(body.SubjectId), o, string(body.Format))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusAccepted, toGenExport(j))
}

func (h *Handlers) GetExport(c *gin.Context, exportID int64) {
	j, err := h.deps.Export.Get(c.Request.Context(), currentUser(c), uint64(exportID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenExport(j))
}

func init() {
	// 邀请开关关闭时 6.8 接口返回 404（ADR 0009）。
	flagGuards["GET "+APIPrefix+"/me/invites"] = "invite"
}

func (h *Handlers) GetInvites(c *gin.Context) {
	o, err := h.deps.Invite.Overview(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.InviteOverview{Code: o.Code, RewardDays: o.RewardDays, MaxDays: o.MaxDays, EarnedDays: o.EarnedDays, Invited: o.Invited,
		Records: make([]gen.InviteRecord, len(o.Records))}
	for i, r := range o.Records {
		out.Records[i] = gen.InviteRecord{RegisteredAt: r.RegisteredAt, Activated: r.Activated, Days: r.Days}
	}
	c.JSON(http.StatusOK, out)
}

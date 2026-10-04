package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"

	"peetraining-server/internal/admin"
	"peetraining-server/internal/gen"
)

// 管理后台：配置与系统（T29）：7.8 AI 与额度、7.9 消息与公告、7.15 后台账号与权限。只有管理员（契约 x-roles）。

func items[T any](xs []T) gin.H {
	if xs == nil {
		xs = []T{}
	}
	return gin.H{"items": xs}
}

func toGenParam(p admin.Param) gen.AdminParam {
	var v map[string]any
	_ = json.Unmarshal(p.Value, &v)
	return gen.AdminParam{Key: p.Key, Value: v, Description: p.Description, Version: p.Version, UpdatedAt: p.UpdatedAt}
}

func (h *Handlers) ListAdminParams(c *gin.Context) {
	ps, err := h.deps.Admin.Params(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.AdminParam, len(ps))
	for i, p := range ps {
		out[i] = toGenParam(p)
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) UpdateAdminParam(c *gin.Context, key string) {
	var body gen.UpdateAdminParamJSONBody
	if !bind(c, &body) {
		return
	}
	raw, _ := json.Marshal(body.Value)
	p, err := h.deps.Admin.UpdateParam(c.Request.Context(), currentAdmin(c).ID, key, raw, body.Version)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenParam(p))
}

func toGenAgreement(a admin.Agreement) gen.AdminAgreement {
	return gen.AdminAgreement{Id: int64(a.ID), Kind: gen.AgreementKind(a.Kind), Version: a.Version, Title: a.Title, Body: opt(a.Body), ChangeSummary: opt(a.ChangeSummary),
		EffectiveAt: a.EffectiveAt, PublishedAt: a.PublishedAt}
}

func (h *Handlers) ListAdminAgreements(c *gin.Context) {
	as, err := h.deps.Admin.Agreements(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.AdminAgreement, len(as))
	for i, a := range as {
		out[i] = toGenAgreement(a)
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) GetAdminAgreement(c *gin.Context, id int64) {
	a, err := h.deps.Admin.Agreement(c.Request.Context(), uint64(id))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenAgreement(a))
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (h *Handlers) CreateAdminAgreement(c *gin.Context) {
	var body gen.CreateAdminAgreementJSONBody
	if !bind(c, &body) {
		return
	}
	a, err := h.deps.Admin.SaveAgreementDraft(c.Request.Context(), admin.Agreement{Kind: string(body.Kind), Version: body.Version, Title: body.Title, Body: body.Body,
		ChangeSummary: deref(body.ChangeSummary), EffectiveAt: body.EffectiveAt})
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenAgreement(a))
}

func (h *Handlers) UpdateAdminAgreement(c *gin.Context, id int64) {
	var body gen.UpdateAdminAgreementJSONBody
	if !bind(c, &body) {
		return
	}
	a, err := h.deps.Admin.SaveAgreementDraft(c.Request.Context(), admin.Agreement{ID: uint64(id), Title: body.Title, Body: body.Body, ChangeSummary: deref(body.ChangeSummary),
		EffectiveAt: body.EffectiveAt})
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenAgreement(a))
}

func (h *Handlers) PublishAdminAgreement(c *gin.Context, id int64) {
	a, err := h.deps.Admin.PublishAgreement(c.Request.Context(), uint64(id))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenAgreement(a))
}

func (h *Handlers) ListExamDates(c *gin.Context) {
	ds, err := h.deps.Admin.ExamDates(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, items(ds2gen(ds)))
}

func ds2gen(ds []admin.ExamDate) []gin.H {
	out := make([]gin.H, len(ds))
	for i, d := range ds {
		out[i] = gin.H{"year": d.Year, "label": d.Label, "first_exam_start": d.FirstExamStart, "first_exam_end": d.FirstExamEnd, "subject_exam_date": d.SubjectExamDate}
	}
	return out
}

func (h *Handlers) SaveExamDate(c *gin.Context, year int) {
	var body gen.SaveExamDateJSONBody
	if !bind(c, &body) {
		return
	}
	err := h.deps.Admin.SaveExamDate(c.Request.Context(), admin.ExamDate{Year: year, Label: deref(body.Label), FirstExamStart: body.FirstExamStart.String(),
		FirstExamEnd: body.FirstExamEnd.String(), SubjectExamDate: body.SubjectExamDate.String()})
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) ListAdminFlags(c *gin.Context) {
	fs, err := h.deps.Admin.Flags(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.AdminFlag, len(fs))
	for i, f := range fs {
		ids := make([]int64, len(f.UserIDs))
		for j, id := range f.UserIDs {
			ids[j] = int64(id)
		}
		out[i] = gen.AdminFlag{Key: f.Key, Description: f.Description, EnabledAll: f.EnabledAll, UserIds: ids, UpdatedAt: f.UpdatedAt}
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) SetAdminFlag(c *gin.Context, key string) {
	var body gen.SetAdminFlagJSONBody
	if !bind(c, &body) {
		return
	}
	ids := make([]uint64, len(body.UserIds))
	for i, id := range body.UserIds {
		ids[i] = uint64(id)
	}
	if err := h.deps.Admin.SetFlag(c.Request.Context(), currentAdmin(c).ID, key, body.EnabledAll, ids); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) ListAppVersions(c *gin.Context) {
	vs, err := h.deps.Admin.AppVersions(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.AdminAppVersion, len(vs))
	for i, v := range vs {
		out[i] = gen.AdminAppVersion{Platform: gen.AdminAppVersionPlatform(v.Platform), Latest: v.Latest, Min: v.Min, DownloadUrl: v.DownloadURL, ReleaseNotes: opt(v.ReleaseNotes), UpdatedAt: v.UpdatedAt}
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) SaveAppVersion(c *gin.Context, platform gen.SaveAppVersionParamsPlatform) {
	var body gen.SaveAppVersionJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Admin.SaveAppVersion(c.Request.Context(), admin.AppVersion{Platform: string(platform), Latest: body.Latest, Min: body.Min, DownloadURL: body.DownloadUrl,
		ReleaseNotes: deref(body.ReleaseNotes)}); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) GetAdminAI(c *gin.Context) {
	o, err := h.deps.Admin.AIOverview(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.AdminAIOverview{CostPerActiveUserDay: fl(o.CostPerActiveUserDay), BudgetPerUserDay: fl(o.BudgetPerUserDay), MonthCostYuan: fl(o.MonthCostYuan),
		MonthRevenueYuan: fl(o.MonthRevenueYuan), RevenueRatio: fl(o.RevenueRatio), RatioMax: fl(o.RatioMax), Tasks: make([]gen.AdminAITask, len(o.Tasks))}
	for i, t := range o.Tasks {
		g := gen.AdminAITask{Capability: t.Capability, StableModel: t.StableModel, StablePrompt: t.StablePrompt, CandidateModel: opt(t.CandidateModel),
			CandidatePrompt: opt(t.CandidatePrompt), CandidatePercent: t.CandidatePct, Prompts: append([]string{}, t.Prompts...), Calls: t.Calls, CostYuan: fl(t.CostYuan),
			CostShare: fl(t.CostShare), Versions: make([]gen.AdminAIVersion, len(t.Versions))}
		for j, v := range t.Versions {
			g.Versions[j] = gen.AdminAIVersion{Model: v.Model, Prompt: v.Prompt, Calls: v.Calls, SuccessRate: fl(v.SuccessRate), AvgCostYuan: fl(v.AvgCostYuan), AvgLatencyMs: v.AvgLatency}
			if v.DisputeRate != nil {
				r := fl(*v.DisputeRate)
				g.Versions[j].DisputeRate = &r
			}
		}
		out.Tasks[i] = g
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) SetAIRollout(c *gin.Context, capability string) {
	var body gen.SetAIRolloutJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Admin.SetRollout(c.Request.Context(), currentAdmin(c).ID, capability, admin.Rollout{StableModel: body.StableModel, StablePrompt: body.StablePrompt,
		CandidateModel: deref(body.CandidateModel), CandidatePrompt: deref(body.CandidatePrompt), CandidatePct: body.CandidatePercent}); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) ListAnnouncements(c *gin.Context) {
	as, err := h.deps.Admin.Announcements(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.AdminAnnouncement, len(as))
	for i, a := range as {
		ids := make([]int64, len(a.UserIDs))
		for j, id := range a.UserIDs {
			ids[j] = int64(id)
		}
		out[i] = gen.AdminAnnouncement{Id: int64(a.ID), Title: a.Title, Body: a.Body, All: a.All, UserIds: ids, WithPopup: a.WithPopup, ScheduledAt: a.ScheduledAt,
			SentAt: a.SentAt, SentCount: a.SentCount, CreatedAt: a.CreatedAt}
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) CreateAnnouncement(c *gin.Context) {
	var body gen.CreateAnnouncementJSONBody
	if !bind(c, &body) {
		return
	}
	a := admin.Announcement{Title: body.Title, Body: body.Body, All: body.All, ScheduledAt: body.ScheduledAt, WithPopup: body.WithPopup != nil && *body.WithPopup}
	if body.UserIds != nil {
		for _, id := range *body.UserIds {
			a.UserIDs = append(a.UserIDs, uint64(id))
		}
	}
	id, err := h.deps.Admin.CreateAnnouncement(c.Request.Context(), currentAdmin(c).ID, a)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.AdminCount{Count: int(id)})
}

func (h *Handlers) CancelAnnouncement(c *gin.Context, id int64) {
	if err := h.deps.Admin.CancelAnnouncement(c.Request.Context(), uint64(id)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func toRoles(rs []string) []admin.Role {
	out := make([]admin.Role, len(rs))
	for i, r := range rs {
		out[i] = admin.Role(r)
	}
	return out
}

func (h *Handlers) ListAdminAccounts(c *gin.Context) {
	as, err := h.deps.Admin.Accounts(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.AdminAccount, len(as))
	for i, a := range as {
		roles := make([]string, len(a.Roles))
		for j, r := range a.Roles {
			roles[j] = string(r)
		}
		out[i] = gen.AdminAccount{Id: int64(a.ID), Username: a.Username, DisplayName: a.DisplayName, PhoneMasked: a.PhoneMasked, Roles: roles,
			Status: gen.AdminAccountStatus(a.Status), MustChangePassword: a.MustChangePassword, LastLoginAt: a.LastLoginAt, CreatedAt: a.CreatedAt}
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) CreateAdminAccount(c *gin.Context) {
	var body gen.CreateAdminAccountJSONBody
	if !bind(c, &body) {
		return
	}
	id, err := h.deps.Admin.CreateAdmin(c.Request.Context(), body.Username, body.DisplayName, body.Phone, body.Password, toRoles(body.Roles), true)
	if isDup(err) {
		_ = c.Error(ErrConflict("账号名已存在"))
		return
	}
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.AdminCount{Count: int(id)})
}

func (h *Handlers) UpdateAdminAccount(c *gin.Context, id int64) {
	var body gen.UpdateAdminAccountJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Admin.UpdateAccount(c.Request.Context(), currentAdmin(c).ID, uint64(id), body.DisplayName, deref(body.Phone), toRoles(body.Roles), body.Active); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) ResetAdminPassword(c *gin.Context, id int64) {
	var body gen.ResetAdminPasswordJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Admin.ResetPassword(c.Request.Context(), uint64(id), body.Password); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

// isDup 判断唯一键冲突（MySQL 1062，如账号名重复）。
func isDup(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

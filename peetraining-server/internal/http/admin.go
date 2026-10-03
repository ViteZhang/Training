package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/oapi-codegen/nullable"

	"peetraining-server/internal/admin"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/quota"
)

// 管理后台（T28）：7.1–7.7。角色由契约 x-roles 声明、AdminGate 检查；写操作由 AdminGate 记审计日志。

func cursorID(c *gen.Cursor) uint64 {
	if c == nil {
		return 0
	}
	v, _ := strconv.ParseUint(*c, 10, 64)
	return v
}

func nextCursor[T any](items []T, id func(T) uint64) *string {
	if len(items) < 50 {
		return nil
	}
	s := strconv.FormatUint(id(items[len(items)-1]), 10)
	return &s
}

func opt[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

func fl(v float64) float32 { return float32(v) }

func bearer(c *gin.Context) string {
	t, _ := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	return t
}

func toGenAdminMe(a admin.Admin) gen.AdminMe {
	out := gen.AdminMe{Id: int64(a.ID), Username: a.Username, DisplayName: a.DisplayName, MustChangePassword: a.MustChangePassword, ExpiresAt: a.ExpiresAt,
		Roles: make([]gen.AdminMeRoles, len(a.Roles))}
	for i, r := range a.Roles {
		out.Roles[i] = gen.AdminMeRoles(r)
	}
	return out
}

// ---------- 账号 ----------

func (h *Handlers) AdminLogin(c *gin.Context) {
	var body gen.AdminLoginJSONBody
	if !bind(c, &body) {
		return
	}
	ch, err := h.deps.Admin.Login(c.Request.Context(), body.Username, body.Password)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.AdminChallenge{ChallengeId: ch.ID, PhoneMasked: ch.PhoneMasked})
}

func (h *Handlers) AdminVerify(c *gin.Context) {
	var body gen.AdminVerifyJSONBody
	if !bind(c, &body) {
		return
	}
	s, err := h.deps.Admin.Verify(c.Request.Context(), body.ChallengeId, body.Code)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.AdminSession{Token: s.Token, Admin: toGenAdminMe(s.Admin)})
}

func (h *Handlers) AdminLogout(c *gin.Context) {
	if err := h.deps.Admin.Logout(c.Request.Context(), bearer(c)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) GetAdminMe(c *gin.Context) { c.JSON(http.StatusOK, toGenAdminMe(currentAdmin(c))) }

func (h *Handlers) ChangeAdminPassword(c *gin.Context) {
	var body gen.ChangeAdminPasswordJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Admin.ChangePassword(c.Request.Context(), currentAdmin(c).ID, body.OldPassword, body.NewPassword); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---------- 7.1 概览 ----------

func named(xs []admin.Named) []gen.AdminNamed {
	out := make([]gen.AdminNamed, len(xs))
	for i, x := range xs {
		out[i] = gen.AdminNamed{Name: x.Name, Value: x.Value}
	}
	return out
}

func (h *Handlers) GetAdminOverview(c *gin.Context) {
	o, err := h.deps.Admin.Overview(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.AdminOverview{UpdatedAt: o.UpdatedAt, UsersTotal: o.UsersTotal, WeekNewUsers: o.WeekNewUsers, WeekActiveUsers: o.WeekActiveUsers, WeekParsePages: o.WeekParsePages,
		WeekParseRate: fl(o.WeekParseRate), PaidUsers: o.PaidUsers, Members: o.Members, Conversion: fl(o.Conversion), MonthRevenueCents: o.MonthRevenue,
		WeekDisputeRate: fl(o.WeekDisputeRate), Funnel: named(o.Funnel), ImportModes: named(o.ImportModes), HotSubjects: named(o.HotSubjects),
		Days: make([]gen.AdminDayPoint, len(o.Days)), Alerts: append([]string{}, o.Alerts...)}
	for i, d := range o.Days {
		out.Days[i] = gen.AdminDayPoint{Day: d.Day, NewUsers: d.NewUsers, ActiveUsers: d.ActiveUsers, Answers: d.Answers, ParsePages: d.ParsePages, RevenueCents: d.RevenueCents}
	}
	c.JSON(http.StatusOK, out)
}

// ---------- 7.2 用户 ----------

func toGenAdminUser(u admin.UserRow) gen.AdminUser {
	return gen.AdminUser{Id: int64(u.ID), PhoneMasked: u.PhoneMasked, InviteCode: u.InviteCode, Status: gen.AdminUserStatus(u.Status), Subjects: strings.TrimSpace(u.Subjects),
		Materials: u.Materials, Questions: u.Questions, IsMember: u.IsMember, CreatedAt: u.CreatedAt, LastActiveAt: u.LastActiveAt}
}

func (h *Handlers) ListAdminUsers(c *gin.Context, p gen.ListAdminUsersParams) {
	q, filter := "", ""
	if p.Q != nil {
		q = strings.TrimSpace(*p.Q)
	}
	if p.Filter != nil {
		filter = string(*p.Filter)
	}
	rows, err := h.deps.Admin.SearchUsers(c.Request.Context(), q, filter, cursorID(p.Cursor))
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.AdminUser, len(rows))
	for i, u := range rows {
		items[i] = toGenAdminUser(u)
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor(rows, func(u admin.UserRow) uint64 { return u.ID })})
}

func toGenQuotaItems(items []quota.Item) []gen.QuotaItem {
	out := make([]gen.QuotaItem, len(items))
	for i, it := range items {
		q := gen.QuotaItem{QuotaType: gen.QuotaType(it.Type), Used: it.Used, Period: gen.QuotaItemPeriod(it.Period)}
		if it.Limit == nil {
			q.Limit = nullable.NewNullNullable[int]()
		} else {
			q.Limit = nullable.NewNullableWithValue(*it.Limit)
		}
		if !it.ResetsAt.IsZero() {
			t := it.ResetsAt
			q.ResetsAt = &t
		}
		out[i] = q
	}
	return out
}

func (h *Handlers) GetAdminUser(c *gin.Context, userID int64) {
	d, err := h.deps.Admin.User(c.Request.Context(), uint64(userID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	u := toGenAdminUser(d.UserRow)
	out := gen.AdminUserDetail{Id: u.Id, PhoneMasked: u.PhoneMasked, InviteCode: u.InviteCode, Status: gen.AdminUserDetailStatus(u.Status), Subjects: u.Subjects,
		Materials: u.Materials, Questions: u.Questions, IsMember: u.IsMember, CreatedAt: u.CreatedAt, LastActiveAt: u.LastActiveAt, Nickname: d.Nickname,
		FailedMaterials: d.FailedMaterials, Pages: d.Pages, KnowledgePoints: d.KnowledgePoints, Papers: d.Papers, WeekGradings: d.WeekGradings, Invited: d.Invited,
		InviteDays: d.InviteDays, MemberTier: opt(d.MemberTier), MemberUntil: d.MemberUntil, Quota: toGenQuotaItems(d.Quota), SubjectStats: make([]gen.AdminSubjectStat, len(d.Subjects))}
	for i, s := range d.Subjects {
		out.SubjectStats[i] = gen.AdminSubjectStat{Name: s.Name, Code: opt(s.Code), FullScore: s.FullScore, TargetScore: s.TargetScore, EstLow: s.EstLow, EstHigh: s.EstHigh}
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) GetAdminUserPhone(c *gin.Context, userID int64) {
	p, err := h.deps.Admin.Phone(c.Request.Context(), uint64(userID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"phone": p})
}

func (h *Handlers) GrantParsePages(c *gin.Context, userID int64) {
	var body gen.GrantParsePagesJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Admin.GrantParsePages(c.Request.Context(), currentAdmin(c).ID, uint64(userID), body.Pages, body.IdempotencyKey); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) GrantMembershipDays(c *gin.Context, userID int64) {
	var body gen.GrantMembershipDaysJSONBody
	if !bind(c, &body) {
		return
	}
	end, err := h.deps.Admin.GrantMembershipDays(c.Request.Context(), currentAdmin(c).ID, uint64(userID), body.Days)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ends_at": end})
}

func (h *Handlers) ReparseUserFailed(c *gin.Context, userID int64) {
	n, err := h.deps.Admin.ReparseFailed(c.Request.Context(), uint64(userID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.AdminCount{Count: n})
}

func (h *Handlers) SendAdminMessage(c *gin.Context, userID int64) {
	var body gen.SendAdminMessageJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Admin.SendMessage(c.Request.Context(), uint64(userID), body.Title, body.Body); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) SetAdminUserStatus(c *gin.Context, userID int64) {
	var body gen.SetAdminUserStatusJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Admin.SetBanned(c.Request.Context(), uint64(userID), body.Banned); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) ListAdminAudit(c *gin.Context, p gen.ListAdminAuditParams) {
	tt, tid := "", ""
	if p.TargetType != nil {
		tt = *p.TargetType
	}
	if p.TargetId != nil {
		tid = *p.TargetId
	}
	rows, err := h.deps.Admin.AuditLogs(c.Request.Context(), tt, tid, cursorID(p.Cursor))
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.AdminAudit, len(rows))
	for i, r := range rows {
		items[i] = gen.AdminAudit{Id: int64(r.ID), AdminName: r.AdminName, Action: r.Action, TargetType: opt(r.TargetType), TargetId: opt(r.TargetID), CreatedAt: r.CreatedAt}
		if r.Detail != nil {
			items[i].Detail = &r.Detail
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor(rows, func(r admin.AuditRow) uint64 { return r.ID })})
}

func (h *Handlers) ListAccessLogs(c *gin.Context, p gen.ListAccessLogsParams) {
	var uid uint64
	if p.UserId != nil {
		uid = uint64(*p.UserId)
	}
	rows, err := h.deps.Admin.AccessLogs(c.Request.Context(), uid)
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.AdminAccessLog, len(rows))
	for i, r := range rows {
		items[i] = gen.AdminAccessLog{Id: int64(r.ID), GrantId: int64(r.GrantID), AdminName: r.AdminName, UserId: int64(r.UserID), TargetType: r.TargetType, TargetId: int64(r.TargetID), CreatedAt: r.CreatedAt}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ---------- 7.3 会员与订单 ----------

func (h *Handlers) ListAdminOrders(c *gin.Context, p gen.ListAdminOrdersParams) {
	status := ""
	if p.Status != nil {
		status = string(*p.Status)
	}
	var uid uint64
	if p.UserId != nil {
		uid = uint64(*p.UserId)
	}
	rows, err := h.deps.Admin.Orders(c.Request.Context(), status, uid, cursorID(p.Cursor))
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.AdminOrder, len(rows))
	for i, o := range rows {
		items[i] = gen.AdminOrder{OrderNo: o.OrderNo, UserId: int64(o.UserID), Tier: gen.PlanTier(o.Tier), Channel: gen.PayChannel(o.Channel), AmountCents: o.AmountCents,
			Status: gen.OrderStatus(o.Status), RefundStatus: opt(o.RefundStatus), PaidAt: o.PaidAt, CreatedAt: o.CreatedAt}
	}
	// 订单列表按内部 ID 翻页；对外只给订单号，cursor 用最后一单的创建时间不稳定，这里用序号。
	var next *string
	if len(rows) == 50 {
		next = ptr(strconv.Itoa(int(cursorID(p.Cursor)) + 50))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

func (h *Handlers) GetAdminOrderSummary(c *gin.Context) {
	s, err := h.deps.Admin.OrderSummary(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.AdminOrderSummary{MonthRevenueCents: s.MonthRevenue, PaidUsers: s.PaidUsers, ImportedUsers: s.ImportedUsers, Conversion: fl(s.Conversion),
		PendingRefunds: s.PendingRefunds, Tiers: make([]gen.AdminTierSales, len(s.Tiers))}
	for i, t := range s.Tiers {
		out.Tiers[i] = gen.AdminTierSales{Tier: gen.PlanTier(t.Tier), Orders: t.Orders, RevenueCents: t.Revenue}
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) GetAdminOrderUsage(c *gin.Context, orderNo string) {
	u, err := h.deps.Admin.OrderUsage(c.Request.Context(), orderNo)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.AdminOrderUsage{Pages: u.Pages, Gradings: u.Gradings, Answers: u.Answers, FailedMaterials: u.FailedMaterials, DaysSincePaid: u.DaysSincePaid})
}

func (h *Handlers) RefundAdminOrder(c *gin.Context, orderNo string) {
	var body gen.RefundAdminOrderJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Admin.Refund(c.Request.Context(), currentAdmin(c).ID, orderNo, body.Reason); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---------- 7.4 兑换码 ----------

func (h *Handlers) GetRedeemSummary(c *gin.Context) {
	s, err := h.deps.Admin.RedeemSummary(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.AdminRedeemSummary{Generated: s.Generated, Used: s.Used, Available: s.Available, Inactive: s.Inactive})
}

func toGenBatch(b admin.Batch) gen.AdminBatch {
	return gen.AdminBatch{Id: int64(b.ID), Name: b.Name, Tier: gen.AdminBatchTier(b.Tier), Days: b.Days, Quantity: b.Quantity, Used: b.Used, ExpiresAt: b.ExpiresAt,
		Channel: b.Channel, Status: gen.AdminBatchStatus(b.Status), CreatedAt: b.CreatedAt}
}

func toGenCode(r admin.CodeRow) gen.AdminCode {
	out := gen.AdminCode{Id: int64(r.ID), Tail: r.Tail, Status: gen.AdminCodeStatus(r.Status), BatchId: int64(r.BatchID), BatchName: r.BatchName, Tier: r.Tier,
		Days: opt(r.Days), ExpiresAt: r.ExpiresAt, UsedAt: r.UsedAt}
	if r.UsedBy != 0 {
		out.UsedBy = ptr(int64(r.UsedBy))
	}
	return out
}

func (h *Handlers) ListRedeemBatches(c *gin.Context, p gen.ListRedeemBatchesParams) {
	rows, err := h.deps.Admin.Batches(c.Request.Context(), cursorID(p.Cursor))
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.AdminBatch, len(rows))
	for i, b := range rows {
		items[i] = toGenBatch(b)
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor(rows, func(b admin.Batch) uint64 { return b.ID })})
}

func (h *Handlers) CreateRedeemBatch(c *gin.Context) {
	var body gen.CreateRedeemBatchJSONBody
	if !bind(c, &body) {
		return
	}
	in := admin.NewBatch{Name: body.Name, Tier: string(body.Tier), Quantity: body.Quantity, ExpiresAt: body.ExpiresAt}
	if body.Days != nil {
		in.Days = *body.Days
	}
	if body.Channel != nil {
		in.Channel = *body.Channel
	}
	b, codes, err := h.deps.Admin.CreateBatch(c.Request.Context(), currentAdmin(c).ID, in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.AdminBatchCreated{Batch: toGenBatch(b), Codes: codes})
}

func (h *Handlers) GetRedeemBatch(c *gin.Context, batchID int64) {
	b, codes, err := h.deps.Admin.BatchCodes(c.Request.Context(), uint64(batchID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.AdminCode, len(codes))
	used := 0
	for i, r := range codes {
		out[i] = toGenCode(r)
		if r.Status == "used" {
			used++
		}
	}
	b.Used = used
	c.JSON(http.StatusOK, gin.H{"batch": toGenBatch(b), "codes": out})
}

func (h *Handlers) DisableRedeemBatch(c *gin.Context, batchID int64) {
	if err := h.deps.Admin.DisableBatch(c.Request.Context(), uint64(batchID)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) FindRedeemCode(c *gin.Context, p gen.FindRedeemCodeParams) {
	r, err := h.deps.Admin.FindCode(c.Request.Context(), p.Code)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenCode(r))
}

func (h *Handlers) VoidRedeemCode(c *gin.Context, codeID int64) {
	if err := h.deps.Admin.VoidCode(c.Request.Context(), uint64(codeID)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---------- 7.5 资料解析监控 ----------

func (h *Handlers) GetParseStats(c *gin.Context) {
	s, err := h.deps.Admin.ParseStats(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.AdminParseStats{SuccessRate: fl(s.SuccessRate), Pages: s.Pages, Files: s.Files, AvgSeconds: s.AvgSeconds, P95Seconds: s.P95Seconds, Formats: make([]gen.AdminFormatStat, len(s.Formats))}
	for i, f := range s.Formats {
		out.Formats[i] = gen.AdminFormatStat{Format: f.Format, Files: f.Files, Pages: f.Pages, Ok: f.OK, Partial: f.Partial, Failed: f.Failed, Rate: fl(f.Rate)}
	}
	c.JSON(http.StatusOK, out)
}

func toGenImportJob(j admin.ImportJobRow) gen.AdminImportJob {
	return gen.AdminImportJob{Id: int64(j.ID), UserId: int64(j.UserID), Mode: j.Mode, Status: j.Status, Files: j.Files, FailedFiles: j.FailedFiles, PartialFiles: j.PartialFiles,
		BilledPages: j.BilledPages, CreatedAt: j.CreatedAt, FinishedAt: j.FinishedAt}
}

func (h *Handlers) ListFailedImports(c *gin.Context, p gen.ListFailedImportsParams) {
	rows, err := h.deps.Admin.FailedImports(c.Request.Context(), cursorID(p.Cursor))
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.AdminImportJob, len(rows))
	for i, j := range rows {
		items[i] = toGenImportJob(j)
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor(rows, func(j admin.ImportJobRow) uint64 { return j.ID })})
}

func (h *Handlers) GetAdminImportJob(c *gin.Context, jobID int64) {
	d, err := h.deps.Admin.ImportJob(c.Request.Context(), uint64(jobID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	j := toGenImportJob(d.ImportJobRow)
	out := gen.AdminImportJobDetail{Id: j.Id, UserId: j.UserId, Mode: j.Mode, Status: j.Status, Files: j.Files, FailedFiles: j.FailedFiles, PartialFiles: j.PartialFiles,
		BilledPages: j.BilledPages, CreatedAt: j.CreatedAt, FinishedAt: j.FinishedAt, ReservedPages: d.ReservedPages, FileLogs: make([]gen.AdminImportFile, len(d.FileLogs))}
	if len(d.PromptVersions) > 0 {
		out.PromptVersions = &d.PromptVersions
	}
	for i, f := range d.FileLogs {
		out.FileLogs[i] = gen.AdminImportFile{MaterialId: int64(f.MaterialID), Format: f.Format, Pages: f.Pages, Step: f.Step, Status: f.Status, Attempts: f.Attempts,
			FailReason: opt(f.FailReason), UpdatedAt: f.UpdatedAt}
		if len(f.FailedPages) > 0 {
			out.FileLogs[i].FailedPages = &f.FailedPages
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) RerunImport(c *gin.Context, jobID int64) {
	n, err := h.deps.Admin.RerunImport(c.Request.Context(), uint64(jobID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.AdminCount{Count: n})
}

func (h *Handlers) SendPhotoTips(c *gin.Context, jobID int64) {
	if err := h.deps.Admin.SendPhotoTips(c.Request.Context(), uint64(jobID)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---------- 7.6 批改异议 ----------

func (h *Handlers) GetDisputeStats(c *gin.Context) {
	s, err := h.deps.Admin.DisputeStats(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.AdminDisputeStats{Disputes: s.Disputes, Gradings: s.Gradings, Rate: fl(s.Rate), ChangedRatio: fl(s.ChangedRatio), AvgSeconds: s.AvgSeconds, Reasons: s.Reasons})
}

func (h *Handlers) ListAdminDisputes(c *gin.Context, p gen.ListAdminDisputesParams) {
	status := ""
	if p.Status != nil {
		status = string(*p.Status)
	}
	rows, err := h.deps.Admin.Disputes(c.Request.Context(), status, cursorID(p.Cursor))
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.AdminDispute, len(rows))
	for i, d := range rows {
		items[i] = gen.AdminDispute{Id: int64(d.ID), UserId: int64(d.UserID), Reason: d.Reason, Status: d.Status, ScoreBefore: f32p(d.ScoreBefore), ScoreAfter: f32p(d.ScoreAfter),
			Attribution: opt(d.Attribution), Granted: d.GrantID != 0, CreatedAt: d.CreatedAt, ResolvedAt: d.ResolvedAt}
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor(rows, func(d admin.DisputeRow) uint64 { return d.ID })})
}

func rawOrNil(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func (h *Handlers) ViewDisputeContent(c *gin.Context, disputeID int64) {
	g, err := h.deps.Admin.ViewDispute(c.Request.Context(), currentAdmin(c).ID, uint64(disputeID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.AdminGrantedGrading{Stem: g.Stem, Qtype: g.QType, Answer: g.Answer, Rubric: rawOrNil(g.Rubric), PointResults: rawOrNil(g.PointResults), Score: f32p(g.Score),
		FullScore: f32p(g.FullScore), GrantExpiresAt: g.ExpiresAt}
	if g.Regrade != nil {
		out.Regrade = &struct {
			PointResults interface{} `json:"point_results,omitempty"`
			Rubric       interface{} `json:"rubric,omitempty"`
			Score        *float32    `json:"score,omitempty"`
		}{PointResults: rawOrNil(g.Regrade.PointResults), Rubric: rawOrNil(g.Regrade.Rubric), Score: f32p(g.Regrade.Score)}
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) AttributeDispute(c *gin.Context, disputeID int64) {
	var body gen.AttributeDisputeJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Admin.AttributeDispute(c.Request.Context(), currentAdmin(c).ID, uint64(disputeID), string(body.Attribution)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---------- 7.7 用户反馈 ----------

func (h *Handlers) GetFeedbackStats(c *gin.Context) {
	s, err := h.deps.Admin.FeedbackStats(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.AdminFeedbackStats{Open: s.Open, AvgReplySeconds: s.AvgReplySeconds, TopType: s.TopType, Satisfaction: fl(s.Satisfaction), Types: s.Types})
}

func (h *Handlers) ListAdminFeedbacks(c *gin.Context, p gen.ListAdminFeedbacksParams) {
	status := ""
	if p.Status != nil {
		status = string(*p.Status)
	}
	rows, err := h.deps.Admin.Feedbacks(c.Request.Context(), status, cursorID(p.Cursor))
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.AdminFeedback, len(rows))
	for i, f := range rows {
		items[i] = gen.AdminFeedback{Id: int64(f.ID), UserId: int64(f.UserID), Type: gen.FeedbackType(f.Type), Content: f.Content, AllowAccess: f.AllowAccess,
			Status: gen.AdminFeedbackStatus(f.Status), CreatedAt: f.CreatedAt, RepliedAt: f.RepliedAt}
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor(rows, func(f admin.FeedbackRow) uint64 { return f.ID })})
}

func (h *Handlers) GetAdminFeedback(c *gin.Context, feedbackID int64) {
	d, err := h.deps.Admin.Feedback(c.Request.Context(), uint64(feedbackID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.AdminFeedbackDetail{Id: int64(d.ID), UserId: int64(d.UserID), Type: gen.FeedbackType(d.Type), Content: d.Content, AllowAccess: d.AllowAccess,
		Status: gen.AdminFeedbackDetailStatus(d.Status), CreatedAt: d.CreatedAt, RepliedAt: d.RepliedAt, Reply: opt(d.Reply), Screenshots: append([]string{}, d.Screenshots...),
		GrantExpiresAt: d.GrantExpiresAt}
	if d.MaterialID != 0 {
		out.MaterialId = ptr(int64(d.MaterialID))
	}
	if d.RelatedImportID != 0 {
		out.RelatedImportId = ptr(int64(d.RelatedImportID))
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) ViewFeedbackMaterial(c *gin.Context, feedbackID int64) {
	m, err := h.deps.Admin.ViewFeedbackMaterial(c.Request.Context(), currentAdmin(c).ID, uint64(feedbackID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.AdminGrantedMaterial{Id: int64(m.ID), Format: m.Format, Pages: m.Pages, Status: m.Status, PageTexts: append([]string{}, m.PageTexts...), GrantExpiresAt: m.ExpiresAt})
}

func (h *Handlers) ReplyFeedback(c *gin.Context, feedbackID int64) {
	var body gen.ReplyFeedbackJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Admin.ReplyFeedback(c.Request.Context(), currentAdmin(c).ID, uint64(feedbackID), body.Reply); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) ReparseFeedbackMaterial(c *gin.Context, feedbackID int64) {
	if err := h.deps.Admin.ReparseFeedbackMaterial(c.Request.Context(), uint64(feedbackID)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

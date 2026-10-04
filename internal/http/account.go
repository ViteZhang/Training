package http

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/auth"
	"peetraining-server/internal/gen"
)

// 模块 0 启动与账号、6.11 账号与安全、6.12 注销（T06）。

func (h *Handlers) GetBootstrap(c *gin.Context, params gen.GetBootstrapParams) {
	b, err := h.deps.Auth.GetBootstrap(c.Request.Context(), h.deps.Flags, currentUser(c), string(params.Platform), params.AppVersion)
	if err != nil {
		_ = c.Error(err)
		return
	}
	resp := gen.Bootstrap{ServerTime: time.Now().UTC(), AppName: h.deps.AppName, Flags: b.Flags}
	if b.Update != nil {
		resp.Update = &gen.AppUpdate{
			LatestVersion: b.Update.Latest, MinVersion: b.Update.Min, DownloadUrl: b.Update.DownloadURL,
			ReleaseNotes: optStr(b.Update.Notes), Force: b.Update.Force, HasUpdate: &b.Update.HasUpdate,
		}
	}
	resp.LoggedIn = &b.LoggedIn
	if b.LoggedIn {
		resp.OnboardingStep = &b.OnboardingStep
		list := make([]gen.AgreementSummary, len(b.Agreements))
		for i, a := range b.Agreements {
			list[i] = agreementSummary(a)
		}
		resp.AgreementsToAccept = &list
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handlers) GetFeatureFlags(c *gin.Context) {
	f, err := h.deps.Flags.ForUser(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.FeatureFlags{Flags: f})
}

func (h *Handlers) SendSmsCode(c *gin.Context) {
	var req gen.SendSmsCodeRequest
	if !bind(c, &req) {
		return
	}
	var (
		res auth.SendResult
		err error
	)
	switch req.Purpose {
	case gen.SmsPurposeLogin:
		res, err = h.deps.Auth.SendLoginCode(c.Request.Context(), req.Phone, req.Agree != nil && *req.Agree, c.ClientIP())
	default:
		uid := currentUser(c)
		if uid == 0 {
			_ = c.Error(ErrUnauthorized())
			return
		}
		res, err = h.deps.Auth.SendChangePhoneCode(c.Request.Context(), uid, req.Phone, auth.Purpose(req.Purpose), c.ClientIP())
	}
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.SendSmsCodeResponse{ResendAfterSeconds: res.ResendAfterSeconds, RemainingToday: res.RemainingToday})
}

func (h *Handlers) Login(c *gin.Context) {
	var req gen.LoginRequest
	if !bind(c, &req) {
		return
	}
	dev := auth.Device{ID: req.Device.DeviceId, Platform: string(req.Device.Platform)}
	if req.Device.DeviceName != nil {
		dev.Name = *req.Device.DeviceName
	}
	invite := ""
	if req.InviteCode != nil {
		invite = *req.InviteCode
	}
	res, err := h.deps.Auth.Login(c.Request.Context(), req.Phone, req.Code, dev, invite)
	if err != nil {
		_ = c.Error(err)
		return
	}
	me, err := h.deps.Auth.GetMe(c.Request.Context(), res.User.ID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.LoginResponse{
		AccessToken: res.Tokens.AccessToken, AccessExpiresAt: res.Tokens.AccessExpiresAt,
		RefreshToken: res.Tokens.RefreshToken, RefreshExpiresAt: res.Tokens.RefreshExpiresAt,
		User: toMe(me), IsNewUser: res.IsNew, DeletionCanceled: &res.DeletionCanceled,
	})
}

func (h *Handlers) RefreshToken(c *gin.Context) {
	var req gen.RefreshRequest
	if !bind(c, &req) {
		return
	}
	t, err := h.deps.Auth.Refresh(c.Request.Context(), req.RefreshToken, req.DeviceId)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.TokenPair{AccessToken: t.AccessToken, AccessExpiresAt: t.AccessExpiresAt, RefreshToken: t.RefreshToken, RefreshExpiresAt: t.RefreshExpiresAt})
}

func (h *Handlers) Logout(c *gin.Context) {
	if err := h.deps.Auth.Logout(c.Request.Context(), currentUser(c), currentDevice(c)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) GetAgreement(c *gin.Context, kind gen.AgreementKind) {
	a, err := h.deps.Auth.GetAgreement(c.Request.Context(), string(kind))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.Agreement{
		Id: int64(a.ID), Kind: gen.AgreementKind(a.Kind), Version: a.Version, Title: a.Title,
		ChangeSummary: optStr(a.ChangeSummary.String), Body: a.Body, EffectiveAt: a.EffectiveAt,
		UpdatedAt: a.PublishedAt.Time,
	})
}

func (h *Handlers) GetMe(c *gin.Context) {
	me, err := h.deps.Auth.GetMe(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toMe(me))
}

func (h *Handlers) UpdateMe(c *gin.Context) {
	var req gen.UpdateMeRequest
	if !bind(c, &req) {
		return
	}
	var step *string
	if req.OnboardingStep != nil {
		s := string(*req.OnboardingStep)
		step = &s
	}
	me, err := h.deps.Auth.UpdateMe(c.Request.Context(), currentUser(c), req.Nickname, step)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toMe(me))
}

func (h *Handlers) AcceptAgreements(c *gin.Context) {
	var req gen.AcceptAgreementsJSONRequestBody
	if !bind(c, &req) {
		return
	}
	if err := h.deps.Auth.AcceptAgreements(c.Request.Context(), currentUser(c), req.AgreementIds); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) ListDevices(c *gin.Context) {
	list, err := h.deps.Auth.Devices(c.Request.Context(), currentUser(c), currentDevice(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.Device, len(list))
	for i, d := range list {
		items[i] = gen.Device{DeviceId: d.ID, DeviceName: d.Name, Platform: gen.DevicePlatform(d.Platform), LastUsedAt: d.LastUsedAt, Current: d.Current}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *Handlers) RemoveDevice(c *gin.Context, deviceID string) {
	if err := h.deps.Auth.RemoveDevice(c.Request.Context(), currentUser(c), deviceID, currentDevice(c)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) ChangePhone(c *gin.Context) {
	var req gen.ChangePhoneRequest
	if !bind(c, &req) {
		return
	}
	if _, err := h.deps.Auth.ChangePhone(c.Request.Context(), currentUser(c), req.OldCode, req.NewPhone, req.NewCode); err != nil {
		_ = c.Error(err)
		return
	}
	h.GetMe(c)
}

func (h *Handlers) RequestDeletion(c *gin.Context) {
	var req gen.RequestDeletionJSONRequestBody
	if !bind(c, &req) {
		return
	}
	if !req.Confirmed {
		_ = c.Error(ErrBadRequest("请先勾选确认"))
		return
	}
	due, err := h.deps.Auth.RequestDeletion(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deletion_due_at": due})
}

func (h *Handlers) CancelDeletion(c *gin.Context) {
	if err := h.deps.Auth.CancelDeletion(c.Request.Context(), currentUser(c)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func toMe(m auth.Me) gen.Me {
	out := gen.Me{
		Id: int64(m.User.ID), PhoneMasked: auth.MaskPhone(m.User.Phone), Nickname: m.User.Nickname,
		InviteCode: m.User.InviteCode, OnboardingStep: m.User.OnboardingStep,
		Membership: gen.MembershipStatus{IsMember: m.Membership.IsMember},
	}
	if m.Membership.IsMember {
		tier := gen.MembershipStatusTier(m.Membership.Tier)
		out.Membership.Tier = &tier
		out.Membership.EndsAt = &m.Membership.EndsAt
	}
	if m.User.DeletionDueAt.Valid {
		out.DeletionDueAt = &m.User.DeletionDueAt.Time
	}
	return out
}

func agreementSummary(a auth.Agreement) gen.AgreementSummary {
	return gen.AgreementSummary{Id: int64(a.ID), Kind: gen.AgreementKind(a.Kind), Version: a.Version, Title: a.Title, ChangeSummary: optStr(a.ChangeSummary.String)}
}

// bind 解析 JSON 请求体；格式已由契约校验中间件检查过，这里只处理解码失败。
func bind(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		_ = c.Error(ErrBadRequest("请求参数不正确").Wrap(err))
		return false
	}
	return true
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

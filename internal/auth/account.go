package auth

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/flags"
)

// Membership 是会员状态（Me 与 6.1 会员条用）。
type Membership struct {
	IsMember bool
	Tier     string
	EndsAt   time.Time
}

// Me 是当前用户信息。
type Me struct {
	User       dbq.User
	Membership Membership
}

// GetMe 返回当前用户与会员状态。
func (s *Service) GetMe(ctx context.Context, userID uint64) (Me, error) {
	u, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return Me{}, notFound(err)
	}
	m, err := s.membership(ctx, userID)
	if err != nil {
		return Me{}, err
	}
	return Me{User: u, Membership: m}, nil
}

func (s *Service) membership(ctx context.Context, userID uint64) (Membership, error) {
	now := s.now().UTC()
	cur, err := s.q.GetCurrentMembership(ctx, dbq.GetCurrentMembershipParams{OwnerUserID: userID, StartsAt: now, EndsAt: now})
	if errors.Is(err, sql.ErrNoRows) {
		return Membership{}, nil
	}
	if err != nil {
		return Membership{}, err
	}
	latest, err := s.q.GetLatestMembershipEnd(ctx, dbq.GetLatestMembershipEndParams{OwnerUserID: userID, EndsAt: now})
	if err != nil {
		return Membership{}, err
	}
	return Membership{IsMember: true, Tier: string(cur.Tier), EndsAt: latest.EndsAt}, nil
}

// onboardingSteps 是引导进度允许的值（PRD 4：引导中途退出，下次启动回到中断的步骤）。
var onboardingSteps = []string{"1.1", "1.2", "1.3", "1.4", "1.5", "1.6", "1.7", "1.8", "done"}

// UpdateMe 修改昵称或引导进度。
func (s *Service) UpdateMe(ctx context.Context, userID uint64, nickname, onboardingStep *string) (Me, error) {
	if nickname != nil {
		if err := s.q.UpdateUserNickname(ctx, dbq.UpdateUserNicknameParams{Nickname: truncate(*nickname, 16), ID: userID}); err != nil {
			return Me{}, err
		}
	}
	if onboardingStep != nil {
		if !slices.Contains(onboardingSteps, *onboardingStep) {
			return Me{}, apperr.New(apperr.BadRequest, "引导步骤不正确")
		}
		if err := s.q.UpdateUserOnboarding(ctx, dbq.UpdateUserOnboardingParams{OnboardingStep: *onboardingStep, ID: userID}); err != nil {
			return Me{}, err
		}
	}
	return s.GetMe(ctx, userID)
}

// Agreement 是协议正文。
type Agreement = dbq.Agreement

// GetAgreement 返回某类协议当前生效的版本（0.4）。
func (s *Service) GetAgreement(ctx context.Context, kind string) (Agreement, error) {
	a, err := s.q.GetLatestAgreement(ctx, dbq.GetLatestAgreementParams{Kind: dbq.AgreementsKind(kind), PublishedAt: sqlTime(s.now().UTC())})
	return a, notFound(err)
}

// AcceptAgreements 记录用户同意了新版本协议（0.4b「同意并继续」）。
func (s *Service) AcceptAgreements(ctx context.Context, userID uint64, ids []int64) error {
	for _, id := range ids {
		if id <= 0 {
			return apperr.NotFoundErr()
		}
		if _, err := s.q.GetPublishedAgreementByID(ctx, uint64(id)); err != nil {
			return notFound(err)
		}
		if err := s.q.AcceptAgreement(ctx, dbq.AcceptAgreementParams{UserID: userID, AgreementID: uint64(id)}); err != nil {
			return err
		}
	}
	return nil
}

// PendingAgreements 返回用户还没同意的当前生效协议（0.4b）。
func (s *Service) PendingAgreements(ctx context.Context, userID uint64) ([]Agreement, error) {
	latest, err := s.q.ListLatestAgreements(ctx, sqlTime(s.now().UTC()))
	if err != nil {
		return nil, err
	}
	accepted, err := s.q.ListAcceptedAgreementIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := []Agreement{}
	for _, a := range latest {
		// 会员服务协议在开通会员时同意，不在启动时弹出。
		if a.Kind == dbq.AgreementsKindMembership || slices.Contains(accepted, a.ID) {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// AppUpdate 是版本检查结果（0.6、0.6b）。
type AppUpdate struct {
	Latest, Min, DownloadURL, Notes string
	Force, HasUpdate                bool
}

// Bootstrap 是 App 启动配置（0.1）。
type Bootstrap struct {
	Update         *AppUpdate
	Flags          map[string]bool
	Agreements     []Agreement
	LoggedIn       bool
	OnboardingStep string
}

// GetBootstrap 返回启动配置；userID 为 0 表示未登录。
func (s *Service) GetBootstrap(ctx context.Context, fl *flags.Service, userID uint64, platform, appVersion string) (Bootstrap, error) {
	var b Bootstrap
	v, err := s.q.GetAppVersion(ctx, dbq.AppVersionsPlatform(platform))
	switch {
	case err == nil:
		b.Update = &AppUpdate{
			Latest: v.LatestVersion, Min: v.MinVersion, DownloadURL: v.DownloadUrl, Notes: v.ReleaseNotes.String,
			Force:     CompareVersions(appVersion, v.MinVersion) < 0,
			HasUpdate: CompareVersions(appVersion, v.LatestVersion) < 0,
		}
	case !errors.Is(err, sql.ErrNoRows):
		return b, err
	}
	if b.Flags, err = fl.ForUser(ctx, userID); err != nil {
		return b, err
	}
	if userID == 0 {
		return b, nil
	}
	u, err := s.q.GetUserByID(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		// 令牌有效但账号已删除：按未登录处理。
		return b, nil
	}
	if err != nil {
		return b, err
	}
	b.LoggedIn = u.Status == dbq.UsersStatusActive
	b.OnboardingStep = u.OnboardingStep
	if b.LoggedIn {
		if b.Agreements, err = s.PendingAgreements(ctx, userID); err != nil {
			return b, err
		}
	}
	return b, nil
}

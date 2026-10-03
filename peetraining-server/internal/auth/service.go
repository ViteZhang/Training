// Package auth 是账号：短信验证码登录（登录即注册）、令牌与设备、协议版本、版本检查、更换手机号、注销冷静期（T06）。
package auth

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/cloud/sms"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/params"
	"peetraining-server/internal/store"
)

// Service 是账号服务。
type Service struct {
	db         *sql.DB
	q          *dbq.Queries
	rdb        *redis.Client
	sms        sms.Sender
	oss        oss.Store
	params     *params.Store
	jwtSecret  []byte
	logCodes   bool
	now        func() time.Time
	log        *slog.Logger
	onRegister func(ctx context.Context, q *dbq.Queries, userID uint64, inviteCode string, now time.Time) error
}

// Deps 是创建服务需要的依赖。
type Deps struct {
	DB        *sql.DB
	Redis     *redis.Client
	SMS       sms.Sender
	OSS       oss.Store
	Params    *params.Store
	JWTSecret string
	// LogCodes 为 true 时把验证码写进日志（只在本地与测试环境用 mock 短信时打开）。
	LogCodes bool
	Logger   *slog.Logger
	Now      func() time.Time
	// OnRegister 在创建账号的事务里调用，带上注册时填的邀请码（T26 绑定邀请关系）；为空时不处理邀请码。
	OnRegister func(ctx context.Context, q *dbq.Queries, userID uint64, inviteCode string, now time.Time) error
}

func New(d Deps) *Service {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		db: d.DB, q: dbq.New(d.DB), rdb: d.Redis, sms: d.SMS, oss: d.OSS, params: d.Params,
		jwtSecret: []byte(d.JWTSecret), logCodes: d.LogCodes, now: now, log: d.Logger, onRegister: d.OnRegister,
	}
}

// Device 是登录设备信息。
type Device struct {
	ID       string
	Name     string
	Platform string // ios / android / web
}

// SendLoginCode 发登录验证码；未同意协议时不发（0.2b）。
func (s *Service) SendLoginCode(ctx context.Context, phone string, agreed bool, ip string) (SendResult, error) {
	if !agreed {
		return SendResult{}, apperr.New(apperr.BadRequest, "请阅读并同意用户协议和隐私政策").With("reason", "agreement_required")
	}
	return s.sendCode(ctx, phone, PurposeLogin, ip)
}

// SendChangePhoneCode 发更换手机号的验证码（6.11）：旧号码必须是当前号码，新号码不能已注册。
func (s *Service) SendChangePhoneCode(ctx context.Context, userID uint64, phone string, purpose Purpose, ip string) (SendResult, error) {
	u, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return SendResult{}, err
	}
	switch purpose {
	case PurposeChangePhoneOld:
		if phone != u.Phone {
			return SendResult{}, apperr.New(apperr.BadRequest, "请填写当前绑定的手机号")
		}
	case PurposeChangePhoneNew:
		if phone == u.Phone {
			return SendResult{}, apperr.New(apperr.BadRequest, "新手机号与当前手机号相同")
		}
		if _, err := s.q.GetUserByPhone(ctx, phone); err == nil {
			return SendResult{}, apperr.New(apperr.Conflict, "这个手机号已经注册过账号")
		} else if !errors.Is(err, sql.ErrNoRows) {
			return SendResult{}, err
		}
	default:
		return SendResult{}, apperr.New(apperr.BadRequest, "验证码用途不正确")
	}
	return s.sendCode(ctx, phone, purpose, ip)
}

// LoginResult 是登录结果。
type LoginResult struct {
	Tokens           TokenPair
	User             dbq.User
	IsNew            bool
	DeletionCanceled bool
}

// Login 用手机号 + 验证码登录；未注册的手机号自动创建账号（PRD 0.2）。
// 注销冷静期内登录即撤销注销（PRD 6.12）。登录时记录用户同意的当前协议版本（勾选协议才能获取验证码）。
// inviteCode 是新用户注册时填的邀请码（老用户登录时忽略）。
func (s *Service) Login(ctx context.Context, phone, code string, dev Device, inviteCode string) (LoginResult, error) {
	if err := s.verifyCode(ctx, phone, PurposeLogin, code); err != nil {
		return LoginResult{}, err
	}
	acc, err := s.params.Account(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	now := s.now().UTC()
	var res LoginResult
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		u, err := q.GetUserByPhone(ctx, phone)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			id, err := createUser(ctx, q, phone)
			if err != nil {
				return err
			}
			if s.onRegister != nil && inviteCode != "" {
				if err := s.onRegister(ctx, q, id, inviteCode, now); err != nil {
					return err
				}
			}
			if u, err = q.GetUserByID(ctx, id); err != nil {
				return err
			}
			res.IsNew = true
		case err != nil:
			return err
		}
		if u.Status == dbq.UsersStatusBanned {
			return apperr.New(apperr.Forbidden, "账号已被封禁，如有疑问请联系客服")
		}
		if u.Status == dbq.UsersStatusDeleting {
			if _, err := q.CancelUserDeletion(ctx, u.ID); err != nil {
				return err
			}
			res.DeletionCanceled = true
			if u, err = q.GetUserByID(ctx, u.ID); err != nil {
				return err
			}
		}
		if err := acceptLatest(ctx, q, u.ID, now); err != nil {
			return err
		}
		// 同一设备重新登录：旧令牌作废。
		if _, err := q.RevokeDeviceTokens(ctx, dbq.RevokeDeviceTokensParams{RevokedAt: sqlTime(now), UserID: u.ID, DeviceID: dev.ID}); err != nil {
			return err
		}
		if err := q.TouchUserActive(ctx, dbq.TouchUserActiveParams{LastActiveAt: sqlTime(now), ID: u.ID}); err != nil {
			return err
		}
		res.User = u
		res.Tokens, err = s.issue(ctx, q, u.ID, dev, now, acc)
		return err
	})
	return res, err
}

func createUser(ctx context.Context, q *dbq.Queries, phone string) (uint64, error) {
	for range 5 {
		code, err := newInviteCode()
		if err != nil {
			return 0, err
		}
		id, err := q.CreateUser(ctx, dbq.CreateUserParams{Phone: phone, InviteCode: code})
		if err == nil {
			return uint64(id), nil
		}
		if !isDuplicate(err) {
			return 0, err
		}
		// 邀请码撞了就换一个；手机号重复（并发注册）时由外层事务失败后客户端重试。
	}
	return 0, errors.New("生成邀请码失败")
}

// acceptLatest 记录用户同意了当前生效的全部协议。
func acceptLatest(ctx context.Context, q *dbq.Queries, userID uint64, now time.Time) error {
	latest, err := q.ListLatestAgreements(ctx, sqlTime(now))
	if err != nil {
		return err
	}
	for _, a := range latest {
		if err := q.AcceptAgreement(ctx, dbq.AcceptAgreementParams{UserID: userID, AgreementID: a.ID}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) issue(ctx context.Context, q *dbq.Queries, userID uint64, dev Device, now time.Time, acc params.Account) (TokenPair, error) {
	access, accessExp, err := s.signAccess(userID, dev.ID, now, time.Duration(acc.AccessTokenMinutes)*time.Minute)
	if err != nil {
		return TokenPair{}, err
	}
	refresh, hash, err := newRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	refreshExp := now.Add(time.Duration(acc.RefreshTokenDays) * 24 * time.Hour)
	platform := dbq.RefreshTokensPlatform(dev.Platform)
	if !platform.Valid() {
		platform = dbq.RefreshTokensPlatformWeb
	}
	err = q.InsertRefreshToken(ctx, dbq.InsertRefreshTokenParams{
		UserID: userID, DeviceID: dev.ID, DeviceName: truncate(dev.Name, 64), Platform: platform,
		TokenHash: hash, ExpiresAt: refreshExp, LastUsedAt: sqlTime(now),
	})
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{AccessToken: access, AccessExpiresAt: accessExp, RefreshToken: refresh, RefreshExpiresAt: refreshExp}, nil
}

var errUnauthorized = apperr.New(apperr.Unauthorized, "登录已失效，请重新登录")

// Refresh 用刷新令牌换新的一对令牌，旧刷新令牌立即作废。
// 已作废的刷新令牌又被使用，说明可能泄露：作废该设备的全部令牌，要求重新登录。
func (s *Service) Refresh(ctx context.Context, refreshToken, deviceID string) (TokenPair, error) {
	acc, err := s.params.Account(ctx)
	if err != nil {
		return TokenPair{}, err
	}
	now := s.now().UTC()
	var out TokenPair
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		rt, err := q.GetRefreshTokenByHash(ctx, hashToken(refreshToken))
		if errors.Is(err, sql.ErrNoRows) {
			return errUnauthorized
		}
		if err != nil {
			return err
		}
		if rt.DeviceID != deviceID || !rt.ExpiresAt.After(now) {
			return errUnauthorized
		}
		if rt.RevokedAt.Valid {
			if _, err := q.RevokeDeviceTokens(ctx, dbq.RevokeDeviceTokensParams{RevokedAt: sqlTime(now), UserID: rt.UserID, DeviceID: rt.DeviceID}); err != nil {
				return err
			}
			return errUnauthorized
		}
		u, err := q.GetUserByID(ctx, rt.UserID)
		if err != nil {
			return err
		}
		if u.Status != dbq.UsersStatusActive {
			return errUnauthorized
		}
		if err := q.RevokeRefreshToken(ctx, dbq.RevokeRefreshTokenParams{RevokedAt: sqlTime(now), ID: rt.ID}); err != nil {
			return err
		}
		if err := q.TouchUserActive(ctx, dbq.TouchUserActiveParams{LastActiveAt: sqlTime(now), ID: u.ID}); err != nil {
			return err
		}
		out, err = s.issue(ctx, q, u.ID, Device{ID: rt.DeviceID, Name: rt.DeviceName, Platform: string(rt.Platform)}, now, acc)
		return err
	})
	// 作废操作要在返回 401 时也生效，所以上面把 401 当作错误回滚了事务；这里对「复用已作废令牌」单独补一次作废。
	if apperr.IsKind(err, apperr.Unauthorized) {
		s.revokeReused(ctx, refreshToken, now)
	}
	return out, err
}

func (s *Service) revokeReused(ctx context.Context, refreshToken string, now time.Time) {
	rt, err := s.q.GetRefreshTokenByHash(ctx, hashToken(refreshToken))
	if err != nil || !rt.RevokedAt.Valid {
		return
	}
	_, _ = s.q.RevokeDeviceTokens(ctx, dbq.RevokeDeviceTokensParams{RevokedAt: sqlTime(now), UserID: rt.UserID, DeviceID: rt.DeviceID})
}

// Logout 作废本设备的令牌。
func (s *Service) Logout(ctx context.Context, userID uint64, deviceID string) error {
	_, err := s.q.RevokeDeviceTokens(ctx, dbq.RevokeDeviceTokensParams{RevokedAt: sqlTime(s.now().UTC()), UserID: userID, DeviceID: deviceID})
	return err
}

// DeviceInfo 是设备列表里的一项。
type DeviceInfo struct {
	ID         string
	Name       string
	Platform   string
	LastUsedAt time.Time
	Current    bool
}

// Devices 返回登录设备列表（6.11）。
func (s *Service) Devices(ctx context.Context, userID uint64, currentDevice string) ([]DeviceInfo, error) {
	rows, err := s.q.ListUserActiveTokens(ctx, dbq.ListUserActiveTokensParams{UserID: userID, ExpiresAt: s.now().UTC()})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []DeviceInfo{}
	for _, r := range rows {
		if seen[r.DeviceID] {
			continue
		}
		seen[r.DeviceID] = true
		out = append(out, DeviceInfo{ID: r.DeviceID, Name: r.DeviceName, Platform: string(r.Platform), LastUsedAt: r.LastUsedAt, Current: r.DeviceID == currentDevice})
	}
	return out, nil
}

// RemoveDevice 移除其他设备（该设备需重新登录）；不能移除本机。
func (s *Service) RemoveDevice(ctx context.Context, userID uint64, deviceID, currentDevice string) error {
	if deviceID == currentDevice {
		return apperr.New(apperr.BadRequest, "不能移除当前设备，请使用退出登录")
	}
	n, err := s.q.RevokeDeviceTokens(ctx, dbq.RevokeDeviceTokensParams{RevokedAt: sqlTime(s.now().UTC()), UserID: userID, DeviceID: deviceID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFoundErr()
	}
	return nil
}

// ChangePhone 更换手机号：旧号码与新号码都要验证（6.11）。
func (s *Service) ChangePhone(ctx context.Context, userID uint64, oldCode, newPhone, newCode string) (dbq.User, error) {
	u, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return dbq.User{}, err
	}
	if newPhone == u.Phone {
		return dbq.User{}, apperr.New(apperr.BadRequest, "新手机号与当前手机号相同")
	}
	if err := s.verifyCode(ctx, u.Phone, PurposeChangePhoneOld, oldCode); err != nil {
		return dbq.User{}, err
	}
	if err := s.verifyCode(ctx, newPhone, PurposeChangePhoneNew, newCode); err != nil {
		return dbq.User{}, err
	}
	if err := s.q.UpdateUserPhone(ctx, dbq.UpdateUserPhoneParams{Phone: newPhone, ID: userID}); err != nil {
		if isDuplicate(err) {
			return dbq.User{}, apperr.New(apperr.Conflict, "这个手机号已经注册过账号")
		}
		return dbq.User{}, err
	}
	return s.q.GetUserByID(ctx, userID)
}

// RequestDeletion 申请注销（6.12）：进入 7 天冷静期并退出全部设备；期间重新登录即撤销。
func (s *Service) RequestDeletion(ctx context.Context, userID uint64) (time.Time, error) {
	acc, err := s.params.Account(ctx)
	if err != nil {
		return time.Time{}, err
	}
	now := s.now().UTC()
	due := now.Add(time.Duration(acc.DeletionCoolingDays) * 24 * time.Hour)
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if err := q.RequestUserDeletion(ctx, dbq.RequestUserDeletionParams{DeletionRequestedAt: sqlTime(now), DeletionDueAt: sqlTime(due), ID: userID}); err != nil {
			return err
		}
		return q.RevokeAllUserTokens(ctx, dbq.RevokeAllUserTokensParams{RevokedAt: sqlTime(now), UserID: userID})
	})
	return due, err
}

// CancelDeletion 撤销注销申请。
func (s *Service) CancelDeletion(ctx context.Context, userID uint64) error {
	_, err := s.q.CancelUserDeletion(ctx, userID)
	return err
}

// PurgeDue 物理删除冷静期已到的账号：先删 OSS 上的全部原件，再删数据库（外键级联删除全部内容）。
// 由定时任务执行；可重复执行，OSS 删除失败的账号留到下次。
func (s *Service) PurgeDue(ctx context.Context, batch int) (int, error) {
	ids, err := s.q.ListUsersDueForDeletion(ctx, dbq.ListUsersDueForDeletionParams{DeletionDueAt: sqlTime(s.now().UTC()), Limit: int32(batch)})
	if err != nil {
		return 0, err
	}
	done := 0
	for _, id := range ids {
		if _, err := s.oss.DeletePrefix(ctx, oss.UserPrefix(id)); err != nil {
			s.log.Error("purge user files failed", "user_id", id, "err", err)
			continue
		}
		if err := s.q.DeleteUser(ctx, id); err != nil {
			s.log.Error("purge user failed", "user_id", id, "err", err)
			continue
		}
		done++
	}
	if done > 0 {
		s.log.Info("purged deleted accounts", "count", done)
	}
	return done, nil
}

func sqlTime(t time.Time) sql.NullTime { return sql.NullTime{Time: t, Valid: true} }

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

// notFound 把 sql.ErrNoRows 转成业务 404，其他错误原样返回。
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	}
	return err
}

package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
)

const (
	// SessionTTL 是后台会话有效期（dev-spec 第十节：8 小时）。
	SessionTTL = 8 * time.Hour
	// challengeTTL 是两步验证码有效期。
	challengeTTL = 5 * time.Minute
	// failLimit 是一个账号 15 分钟内密码或验证码错误的上限，超过后暂停登录。
	failLimit  = 5
	failWindow = 15 * time.Minute
	// maxCodeAttempts 是一个两步验证码最多可输错几次。
	maxCodeAttempts = 5
)

var errBadLogin = apperr.New(apperr.Unauthorized, "账号或密码错误")

func failKey(username string) string { return "admin:fail:" + strings.ToLower(username) }

func challengeKey(id string) string { return "admin:2fa:" + id }

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func sixDigits() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1_000_000))
	return fmt.Sprintf("%06d", n.Int64())
}

var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte(randomHex(8)), bcrypt.DefaultCost)
	return h
})

func hashToken(token string) string {
	h := sha256.Sum256([]byte("admin-session:" + token))
	return hex.EncodeToString(h[:])
}

func (s *Service) checkLocked(ctx context.Context, username string) error {
	n, err := s.d.Redis.Get(ctx, failKey(username)).Int()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	if n >= failLimit {
		return apperr.New(apperr.TooManyRequests, "错误次数太多，请 15 分钟后再试")
	}
	return nil
}

func (s *Service) recordFail(ctx context.Context, username string) {
	k := failKey(username)
	if n, _ := s.d.Redis.Incr(ctx, k).Result(); n == 1 {
		s.d.Redis.Expire(ctx, k, failWindow)
	}
}

// Challenge 是密码校验通过后的两步验证（短信已发到账号绑定的手机）。
type Challenge struct {
	ID          string
	PhoneMasked string
}

// Login 第一步：校验账号密码，通过后给账号绑定的手机发短信验证码（所有后台账号强制两步验证）。
func (s *Service) Login(ctx context.Context, username, password string) (Challenge, error) {
	if err := s.checkLocked(ctx, username); err != nil {
		return Challenge{}, err
	}
	a, err := s.q.GetAdminByUsername(ctx, username)
	if errors.Is(err, sql.ErrNoRows) {
		// 账号不存在时也算一次 bcrypt，响应时间与密码错误一致，不暴露账号是否存在。
		_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(password))
		s.recordFail(ctx, username)
		return Challenge{}, errBadLogin
	}
	if err != nil {
		return Challenge{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(password)) != nil {
		s.recordFail(ctx, username)
		return Challenge{}, errBadLogin
	}
	if a.Status != dbq.AdminUsersStatusActive {
		return Challenge{}, apperr.New(apperr.Forbidden, "账号已停用")
	}
	id, code := randomHex(16), sixDigits()
	if err := s.d.Redis.HSet(ctx, challengeKey(id), map[string]any{"admin_id": a.ID, "code": code, "username": a.Username}).Err(); err != nil {
		return Challenge{}, err
	}
	s.d.Redis.Expire(ctx, challengeKey(id), challengeTTL)
	if err := s.d.SMS.SendCode(ctx, a.Phone, code); err != nil {
		return Challenge{}, fmt.Errorf("发送两步验证码：%w", err)
	}
	if s.d.LogCodes {
		s.d.Log.InfoContext(ctx, "admin 2fa code (mock sms)", "username", a.Username, "code", code)
	}
	return Challenge{ID: id, PhoneMasked: MaskPhone(a.Phone)}, nil
}

// Session 是登录结果。
type Session struct {
	Token string
	Admin Admin
}

// Verify 第二步：校验短信验证码，签发 8 小时的会话令牌（库里只存哈希）。
func (s *Service) Verify(ctx context.Context, challengeID, code string) (Session, error) {
	key := challengeKey(challengeID)
	vals, err := s.d.Redis.HGetAll(ctx, key).Result()
	if err != nil {
		return Session{}, err
	}
	if vals["code"] == "" {
		return Session{}, apperr.New(apperr.BadRequest, "验证码已失效，请重新登录").With("reason", "code_expired")
	}
	if err := s.checkLocked(ctx, vals["username"]); err != nil {
		return Session{}, err
	}
	if subtle.ConstantTimeCompare([]byte(vals["code"]), []byte(code)) != 1 {
		s.recordFail(ctx, vals["username"])
		if n, _ := s.d.Redis.HIncrBy(ctx, key, "attempts", 1).Result(); n >= maxCodeAttempts {
			s.d.Redis.Del(ctx, key)
		}
		return Session{}, apperr.New(apperr.BadRequest, "验证码错误").With("reason", "code_wrong")
	}
	s.d.Redis.Del(ctx, key)
	s.d.Redis.Del(ctx, failKey(vals["username"]))
	var adminID uint64
	if _, err := fmt.Sscan(vals["admin_id"], &adminID); err != nil {
		return Session{}, err
	}
	now := s.now().UTC()
	token := randomHex(32)
	if err := s.q.InsertAdminSession(ctx, dbq.InsertAdminSessionParams{TokenHash: hashToken(token), AdminID: adminID, ExpiresAt: now.Add(SessionTTL), CreatedAt: now}); err != nil {
		return Session{}, err
	}
	_ = s.q.TouchAdminLogin(ctx, dbq.TouchAdminLoginParams{LastLoginAt: sql.NullTime{Time: now, Valid: true}, ID: adminID})
	_ = s.q.DeleteExpiredAdminSessions(ctx, now)
	a, err := s.Authenticate(ctx, token)
	if err != nil {
		return Session{}, err
	}
	if err := s.Audit(ctx, a.ID, "login", "admin", fmt.Sprint(a.ID), nil, ""); err != nil {
		return Session{}, err
	}
	return Session{Token: token, Admin: a}, nil
}

// ErrNoSession 表示后台会话无效或已过期。
var ErrNoSession = errors.New("admin: 会话无效")

// Authenticate 按令牌找会话；过期或账号停用时返回 ErrNoSession。
func (s *Service) Authenticate(ctx context.Context, token string) (Admin, error) {
	row, err := s.q.GetAdminSession(ctx, dbq.GetAdminSessionParams{TokenHash: hashToken(token), ExpiresAt: s.now().UTC()})
	if errors.Is(err, sql.ErrNoRows) {
		return Admin{}, ErrNoSession
	}
	if err != nil {
		return Admin{}, err
	}
	var roles []Role
	_ = json.Unmarshal(row.Roles, &roles)
	return Admin{ID: row.ID, Username: row.Username, DisplayName: row.DisplayName, Roles: roles, MustChangePassword: row.MustChangePassword, ExpiresAt: row.ExpiresAt}, nil
}

// Logout 删除会话。
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.q.DeleteAdminSession(ctx, hashToken(token))
}

// ValidatePassword 要求后台密码至少 10 位，同时含字母和数字。
func ValidatePassword(p string) error {
	var letter, digit bool
	for _, r := range p {
		letter = letter || unicode.IsLetter(r)
		digit = digit || unicode.IsDigit(r)
	}
	if len(p) < 10 || !letter || !digit {
		return apperr.New(apperr.BadRequest, "密码至少 10 位，需同时包含字母和数字")
	}
	return nil
}

// ChangePassword 修改自己的密码（首次登录必须修改初始密码）。
func (s *Service) ChangePassword(ctx context.Context, adminID uint64, oldPassword, newPassword string) error {
	a, err := s.q.GetAdminByID(ctx, adminID)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(oldPassword)) != nil {
		return apperr.New(apperr.BadRequest, "原密码不正确")
	}
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.q.UpdateAdminPassword(ctx, dbq.UpdateAdminPasswordParams{PasswordHash: string(h), ID: adminID})
}

// CreateAdmin 新建后台账号（命令行建第一个管理员；7.15 在 T29）。mustChange 为 true 时首次登录须改密码。
func (s *Service) CreateAdmin(ctx context.Context, username, displayName, phone, password string, roles []Role, mustChange bool) (uint64, error) {
	if err := ValidatePassword(password); err != nil {
		return 0, err
	}
	for _, r := range roles {
		if !validRole(r) {
			return 0, apperr.New(apperr.BadRequest, "未知角色："+string(r))
		}
	}
	if len(roles) == 0 {
		return 0, apperr.New(apperr.BadRequest, "至少选一个角色")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	rb, _ := json.Marshal(roles)
	id, err := s.q.InsertAdmin(ctx, dbq.InsertAdminParams{Username: username, DisplayName: displayName, PasswordHash: string(h), Phone: phone, Roles: rb, MustChangePassword: mustChange})
	return uint64(id), err
}

func validRole(r Role) bool {
	for _, x := range AllRoles {
		if x == r {
			return true
		}
	}
	return false
}

// Audit 写一条后台操作日志（保留 180 天、不能删除；detail 不含用户内容原文）。
func (s *Service) Audit(ctx context.Context, adminID uint64, action, targetType, targetID string, detail map[string]any, ip string) error {
	var d []byte
	if len(detail) > 0 {
		d, _ = json.Marshal(detail)
	}
	return s.q.InsertAdminAudit(ctx, dbq.InsertAdminAuditParams{AdminID: adminID, Action: action, TargetType: sql.NullString{String: targetType, Valid: targetType != ""},
		TargetID: sql.NullString{String: targetID, Valid: targetID != ""}, Detail: d, Ip: sql.NullString{String: ip, Valid: ip != ""}, CreatedAt: s.now().UTC()})
}

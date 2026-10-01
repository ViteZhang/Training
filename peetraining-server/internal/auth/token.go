package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenPair 是登录与刷新返回的令牌。
type TokenPair struct {
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
}

// Claims 是访问令牌的内容：用户 ID 与设备 ID。
type Claims struct {
	DeviceID string `json:"did"`
	jwt.RegisteredClaims
}

const issuer = "training"

// ErrInvalidToken 表示访问令牌无效或过期。
var ErrInvalidToken = errors.New("访问令牌无效")

func (s *Service) signAccess(userID uint64, deviceID string, now time.Time, ttl time.Duration) (string, time.Time, error) {
	exp := now.Add(ttl)
	claims := Claims{
		DeviceID: deviceID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   strconv.FormatUint(userID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
	return tok, exp, err
}

// ParseAccess 校验访问令牌，返回用户 ID 与设备 ID。
func (s *Service) ParseAccess(token string) (uint64, string, error) {
	var c Claims
	_, err := jwt.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return s.jwtSecret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithTimeFunc(s.now),
	)
	if err != nil {
		return 0, "", ErrInvalidToken
	}
	id, err := strconv.ParseUint(c.Subject, 10, 64)
	if err != nil || id == 0 {
		return 0, "", ErrInvalidToken
	}
	return id, c.DeviceID, nil
}

// newRefreshToken 生成随机刷新令牌；数据库只存它的 SHA-256。
func newRefreshToken() (token, hash string, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b[:])
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

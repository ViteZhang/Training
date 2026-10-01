package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/rules"
)

// Purpose 是验证码用途，不同用途的验证码互不通用。
type Purpose string

const (
	PurposeLogin          Purpose = "login"
	PurposeChangePhoneOld Purpose = "change_phone_old"
	PurposeChangePhoneNew Purpose = "change_phone_new"
)

// ipHourlyLimit 是同一 IP 每小时最多发送的验证码条数，防止被用来刷短信。
const ipHourlyLimit = 30

func codeKey(p Purpose, phone string) string { return fmt.Sprintf("sms:code:%s:%s", p, phone) }
func cooldownKey(phone string) string        { return "sms:cd:" + phone }
func dailyKey(phone string, day rules.Day) string {
	return fmt.Sprintf("sms:day:%s:%s", phone, day)
}
func ipKey(ip string, t time.Time) string {
	return fmt.Sprintf("sms:ip:%s:%s", ip, t.UTC().Format("2006010215"))
}

// SendResult 是发送结果。
type SendResult struct {
	ResendAfterSeconds int
	RemainingToday     int
}

// sendCode 发一条验证码（PRD 0.3）：6 位、5 分钟有效、60 秒一次、同一手机号每天最多 10 次。
func (s *Service) sendCode(ctx context.Context, phone string, purpose Purpose, ip string) (SendResult, error) {
	acc, err := s.params.Account(ctx)
	if err != nil {
		return SendResult{}, err
	}
	now := s.now()

	if ip != "" {
		n, err := s.rdb.Incr(ctx, ipKey(ip, now)).Result()
		if err != nil {
			return SendResult{}, err
		}
		if n == 1 {
			s.rdb.Expire(ctx, ipKey(ip, now), time.Hour)
		}
		if n > ipHourlyLimit {
			return SendResult{}, apperr.New(apperr.TooManyRequests, "操作太频繁，请稍后再试").With("reason", "ip_limit")
		}
	}

	resend := time.Duration(acc.SMSResendSeconds) * time.Second
	ok, err := s.rdb.SetNX(ctx, cooldownKey(phone), 1, resend).Result()
	if err != nil {
		return SendResult{}, err
	}
	if !ok {
		ttl, _ := s.rdb.TTL(ctx, cooldownKey(phone)).Result()
		return SendResult{}, apperr.New(apperr.TooManyRequests, "验证码发送太频繁，请稍后再试").
			With("reason", "cooldown").With("retry_after_seconds", int(ttl.Seconds()))
	}

	dk := dailyKey(phone, rules.DayOf(now))
	count, err := s.rdb.Incr(ctx, dk).Result()
	if err != nil {
		return SendResult{}, err
	}
	if count == 1 {
		s.rdb.Expire(ctx, dk, 48*time.Hour)
	}
	if count > int64(acc.SMSDailyLimit) {
		s.rdb.Decr(ctx, dk)
		s.rdb.Del(ctx, cooldownKey(phone))
		return SendResult{}, apperr.New(apperr.TooManyRequests, "今天获取验证码的次数已达上限，请明天再试").
			With("reason", "daily_limit").With("remaining_today", 0)
	}

	code, err := randomCode()
	if err != nil {
		return SendResult{}, err
	}
	key := codeKey(purpose, phone)
	pipe := s.rdb.TxPipeline()
	pipe.Del(ctx, key)
	pipe.HSet(ctx, key, "code", code, "attempts", 0)
	pipe.Expire(ctx, key, time.Duration(acc.SMSCodeTTLSeconds)*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return SendResult{}, err
	}

	if err := s.sms.SendCode(ctx, phone, code); err != nil {
		// 没发出去：撤销本次计数与冷却，用户可以马上重试。
		s.rdb.Del(ctx, key, cooldownKey(phone))
		s.rdb.Decr(ctx, dk)
		return SendResult{}, apperr.New(apperr.BadRequest, "验证码发送失败，请稍后再试").Wrap(err)
	}
	if s.logCodes {
		// 只在本地与测试环境（mock 短信）打印，方便开发登录；生产不会走到这里。
		logx.From(ctx).Info("mock sms code", "phone", phone, "purpose", string(purpose), "code", code)
	}
	return SendResult{ResendAfterSeconds: acc.SMSResendSeconds, RemainingToday: acc.SMSDailyLimit - int(count)}, nil
}

// verifyCode 校验验证码（PRD 0.3b）：错误时清空并提示剩余次数；连续错 5 次该码失效；验证成功后作废。
func (s *Service) verifyCode(ctx context.Context, phone string, purpose Purpose, code string) error {
	acc, err := s.params.Account(ctx)
	if err != nil {
		return err
	}
	key := codeKey(purpose, phone)
	vals, err := s.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return err
	}
	stored, ok := vals["code"]
	if !ok {
		return apperr.New(apperr.BadRequest, "验证码已失效，请重新获取").With("reason", "code_expired").With("code_expired", true)
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(code)) == 1 {
		s.rdb.Del(ctx, key)
		return nil
	}
	attempts, err := s.rdb.HIncrBy(ctx, key, "attempts", 1).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	remaining := acc.SMSMaxAttempts - int(attempts)
	if remaining <= 0 {
		s.rdb.Del(ctx, key)
		return apperr.New(apperr.BadRequest, "验证码错误次数过多，请重新获取").
			With("reason", "code_expired").With("code_expired", true).With("remaining_attempts", 0)
	}
	return apperr.New(apperr.BadRequest, "验证码错误，还可以输入 "+strconv.Itoa(remaining)+" 次").
		With("reason", "code_wrong").With("remaining_attempts", remaining)
}

func randomCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

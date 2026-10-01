// Package params 读取 rule_params（规则参数、额度、价格等），带短时缓存。
//
// 后台 7.8 改了参数后，最多 TTL 之后生效（T29 验收：改免费批改次数后 App 下一次请求即生效，
// 改参数的接口会调用 Invalidate 立即失效）。
package params

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"peetraining-server/internal/dbq"
	"peetraining-server/internal/rules"
)

// TTL 是缓存有效期。
const TTL = 30 * time.Second

// Store 缓存全部 rule_params。
type Store struct {
	q   dbq.Querier
	now func() time.Time

	mu       sync.Mutex
	loadedAt time.Time
	raw      map[string]json.RawMessage
	rules    rules.Params
}

func New(q dbq.Querier) *Store {
	return &Store{q: q, now: time.Now}
}

func (s *Store) load(ctx context.Context) error {
	if s.raw != nil && s.now().Sub(s.loadedAt) < TTL {
		return nil
	}
	rows, err := s.q.ListRuleParams(ctx)
	if err != nil {
		return fmt.Errorf("读取 rule_params：%w", err)
	}
	raw := make(map[string]json.RawMessage, len(rows))
	for _, r := range rows {
		raw[r.ParamKey] = r.Value
	}
	p, err := rules.ParseParams(raw)
	if err != nil {
		return err
	}
	s.raw, s.rules, s.loadedAt = raw, p, s.now()
	return nil
}

// Rules 返回 PRD 第 11 节的规则参数。
func (s *Store) Rules(ctx context.Context) (rules.Params, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(ctx); err != nil {
		return rules.Params{}, err
	}
	return s.rules, nil
}

// Get 把某个键的 JSON 解码到 dst（如 account、quota、pricing）；键不存在时返回错误。
func (s *Store) Get(ctx context.Context, key string, dst any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(ctx); err != nil {
		return err
	}
	raw, ok := s.raw[key]
	if !ok {
		return fmt.Errorf("rule_params 缺少 %s", key)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("rule_params.%s 格式错误：%w", key, err)
	}
	return nil
}

// Invalidate 让下次读取重新加载。
func (s *Store) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.raw = nil
}

// Account 是 rule_params.account。
type Account struct {
	SMSCodeTTLSeconds   int `json:"sms_code_ttl_seconds"`
	SMSResendSeconds    int `json:"sms_resend_seconds"`
	SMSDailyLimit       int `json:"sms_daily_limit"`
	SMSMaxAttempts      int `json:"sms_max_attempts"`
	DeletionCoolingDays int `json:"deletion_cooling_days"`
	AccessTokenMinutes  int `json:"access_token_minutes"`
	RefreshTokenDays    int `json:"refresh_token_days"`
}

// Account 读取账号相关参数。
func (s *Store) Account(ctx context.Context) (Account, error) {
	var a Account
	err := s.Get(ctx, "account", &a)
	return a, err
}

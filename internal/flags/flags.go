// Package flags 是功能开关（PRD 3.4，ADR 0009）：全部打开，或只对指定用户打开。
// 关闭的功能，相关接口返回 404（由 T07 的中间件按开关拦截）。
package flags

import (
	"context"
	"fmt"
	"sync"
	"time"

	"peetraining-server/internal/dbq"
)

// 功能开关名，与 feature_flags.flag_key 一致。
const (
	OnlinePayment = "online_payment"
	OfficialBank  = "official_bank"
	OralRecite    = "oral_recite"
	VoiceAnswer   = "voice_answer"
	ScannedPDF    = "scanned_pdf"
	Invite        = "invite"
)

const ttl = 30 * time.Second

// Service 读取开关，全局开关带短时缓存，指定用户的开关每次查。
type Service struct {
	q   dbq.Querier
	now func() time.Time

	mu       sync.Mutex
	loadedAt time.Time
	global   map[string]bool
}

func New(q dbq.Querier) *Service { return &Service{q: q, now: time.Now} }

func (s *Service) globals(ctx context.Context) (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.global != nil && s.now().Sub(s.loadedAt) < ttl {
		return s.global, nil
	}
	rows, err := s.q.ListFeatureFlags(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取功能开关：%w", err)
	}
	g := make(map[string]bool, len(rows))
	for _, r := range rows {
		g[r.FlagKey] = r.EnabledForAll
	}
	s.global, s.loadedAt = g, s.now()
	return g, nil
}

// ForUser 返回某用户可见的全部开关；userID 为 0 表示未登录，只看全局开关。
func (s *Service) ForUser(ctx context.Context, userID uint64) (map[string]bool, error) {
	g, err := s.globals(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(g))
	for k, v := range g {
		out[k] = v
	}
	if userID == 0 {
		return out, nil
	}
	keys, err := s.q.ListUserFeatureFlags(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("读取用户功能开关：%w", err)
	}
	for _, k := range keys {
		out[k] = true
	}
	return out, nil
}

// Enabled 判断某开关对某用户是否打开。
func (s *Service) Enabled(ctx context.Context, key string, userID uint64) (bool, error) {
	m, err := s.ForUser(ctx, userID)
	if err != nil {
		return false, err
	}
	return m[key], nil
}

// Invalidate 让下次读取重新加载（后台改开关后调用）。
func (s *Service) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.global = nil
}

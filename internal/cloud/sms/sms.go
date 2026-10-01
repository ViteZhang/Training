// Package sms 发送短信验证码。短信只用于验证码（PRD v3 7.9）。
package sms

import (
	"context"
	"sync"
)

// Sender 发送验证码短信。
type Sender interface {
	SendCode(ctx context.Context, phone, code string) error
}

// Mock 不真正发送，只记录最后一条，供本地开发和测试读取验证码。
type Mock struct {
	mu   sync.Mutex
	last map[string]string
}

func NewMock() *Mock { return &Mock{last: map[string]string{}} }

func (m *Mock) SendCode(_ context.Context, phone, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.last[phone] = code
	return nil
}

// LastCode 返回发给某手机号的最后一个验证码。
func (m *Mock) LastCode(phone string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.last[phone]
	return c, ok
}

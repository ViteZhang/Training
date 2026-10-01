// Package ai 是大模型调用的最底层：发请求、拿文本。
//
// 按能力的提示词、JSON Schema 校验、重试与灰度在 internal/ai（T10）里做，业务代码不直接用这个包。
package ai

import (
	"context"
	"fmt"
	"sync"
)

// Message 是一条对话消息。
type Message struct {
	Role    string // system / user / assistant
	Content string
}

// Request 是一次模型调用。
type Request struct {
	// Capability 是 PRD v3 12.1 的能力名（如 grading、import_structure），用于计费与 mock 路由。
	Capability string
	Model      string
	Messages   []Message
	// JSONMode 要求模型只输出 JSON。
	JSONMode    bool
	Temperature float64
}

// Response 是模型返回。
type Response struct {
	Content      string
	Model        string
	InputTokens  int
	OutputTokens int
}

// Client 调用大模型。
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// Mock 按能力返回预设内容，没有预设时返回空 JSON 对象。开发期默认用它（dev-spec 第七节）。
type Mock struct {
	mu        sync.Mutex
	responses map[string]string
	calls     []Request
}

func NewMock() *Mock { return &Mock{responses: map[string]string{}} }

// Set 为某个能力设置固定返回内容。
func (m *Mock) Set(capability, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[capability] = content
}

// Calls 返回收到的全部请求，供测试断言。
func (m *Mock) Calls() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Request(nil), m.calls...)
}

func (m *Mock) Complete(ctx context.Context, req Request) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	content, ok := m.responses[req.Capability]
	if !ok {
		content = "{}"
	}
	return Response{Content: content, Model: fmt.Sprintf("mock-%s", req.Capability)}, nil
}

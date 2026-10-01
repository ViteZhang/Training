// Package asr 把录音转成文字，用于语音作答与口述背诵（都受功能开关控制，默认关闭）。
package asr

import "context"

// Audio 是 OSS 上的一段录音。
type Audio struct {
	ObjectKey string
	Format    string // m4a / wav 等
}

// Transcriber 语音转文字。
type Transcriber interface {
	Transcribe(ctx context.Context, a Audio) (string, error)
}

// Mock 返回固定文本。
type Mock struct{ Text string }

func NewMock() *Mock { return &Mock{Text: "（mock 语音识别结果）"} }

func (m *Mock) Transcribe(ctx context.Context, _ Audio) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return m.Text, nil
}

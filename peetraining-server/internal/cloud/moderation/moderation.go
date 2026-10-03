// Package moderation 对上传资料与 AI 输出做内容安全审核（dev-spec 第九节，内测前必须接入）。
package moderation

import (
	"context"
	"strings"
)

// Verdict 是审核结论。
type Verdict struct {
	Pass bool
	// Reason 是不通过时面向用户的原因。
	Reason string
}

// Checker 审核文本与图片。
type Checker interface {
	CheckText(ctx context.Context, text string) (Verdict, error)
	CheckImage(ctx context.Context, objectKey string) (Verdict, error)
}

// MockBlockWord 出现在文本里时 mock 判为不通过，供测试「审核拒绝」路径（T08 验收）。
const MockBlockWord = "MOCK_BLOCK"

// Mock 默认全部通过；含 MockBlockWord 的文本或对象键判为不通过。
type Mock struct{}

func NewMock() Mock { return Mock{} }

func (Mock) CheckText(ctx context.Context, text string) (Verdict, error) {
	return check(ctx, text)
}

func (Mock) CheckImage(ctx context.Context, objectKey string) (Verdict, error) {
	return check(ctx, objectKey)
}

func check(ctx context.Context, s string) (Verdict, error) {
	if err := ctx.Err(); err != nil {
		return Verdict{}, err
	}
	if strings.Contains(s, MockBlockWord) {
		return Verdict{Pass: false, Reason: "内容未通过安全审核"}, nil
	}
	return Verdict{Pass: true}, nil
}

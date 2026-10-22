package ocr

import (
	"errors"
	"testing"

	"github.com/alibabacloud-go/tea/tea"
)

func TestParseResult(t *testing.T) {
	r, err := parseResult(`{"content":"一、名词解释 1.意境","prism_wordsInfo":[{"word":"一、名词解释","prob":99},{"word":" 1.意境 ","prob":60},{"word":"","prob":10}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "一、名词解释\n1.意境" {
		t.Errorf("按行拼接：%q", r.Text)
	}
	// 「1.意境」从第 7 个字符开始（6 个字 + 换行）。
	if len(r.LowConfidence) != 1 || r.LowConfidence[0] != (Span{Start: 7, End: 11}) {
		t.Errorf("低置信度位置：%+v", r.LowConfidence)
	}
	r, _ = parseResult(`{"content":" 只有全文 "}`)
	if r.Text != "只有全文" {
		t.Errorf("没有逐行信息时用全文：%q", r.Text)
	}
	if _, err := parseResult("not json"); err == nil {
		t.Error("格式不对应报错")
	}
}

func TestSDKError(t *testing.T) {
	bad := sdkError(&tea.SDKError{Code: tea.String("illegalImageFormat"), Message: tea.String("x")})
	if !errors.Is(bad, ErrBadImage) {
		t.Errorf("图片格式问题应为 ErrBadImage：%v", bad)
	}
	other := sdkError(&tea.SDKError{Code: tea.String("Throttling.User"), Message: tea.String("x")})
	if errors.Is(other, ErrBadImage) {
		t.Errorf("限流应可重试：%v", other)
	}
}

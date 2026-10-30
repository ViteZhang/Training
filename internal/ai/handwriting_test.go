package ai

import (
	"context"
	"strings"
	"testing"

	cloudai "peetraining-server/internal/cloud/ai"
)

func TestHandwritingVisionInputAndUnicodeSpans(t *testing.T) {
	c := cloudai.NewMock()
	c.Set(RecognizeHandwriting.Name, `{"pages":[{"segments":[{"text":"意境😀是","uncertain":false},{"text":"交融□","uncertain":true}]},{"segments":[]}]}`)
	e := NewEngine(Config{Client: c})
	images := []cloudai.Image{{Data: []byte("private-image-1")}, {Data: []byte("private-image-2")}}
	out, meta, err := RecognizeHandwriting.Run(context.Background(), e, 5, HandwritingIn{Images: images})
	if err != nil {
		t.Fatal(err)
	}
	calls := c.Calls()
	if len(calls) != 1 || calls[0].Capability != "handwriting_recognize" || meta.Model != "qwen3.8-flash" || !calls[0].JSONMode {
		t.Fatalf("calls=%+v meta=%+v", calls, meta)
	}
	msg := calls[0].Messages[1]
	if len(msg.Images) != 2 || string(msg.Images[0].Data) != "private-image-1" || string(msg.Images[1].Data) != "private-image-2" || strings.Contains(msg.Content, "private-image") {
		t.Fatal("图片应按顺序附到消息，不放进文字提示词")
	}
	text, spans := out.Pages[0].TextAndSpans()
	if text != "意境😀是交融□" || len(spans) != 1 || spans[0] != [2]int{4, 7} {
		t.Fatalf("%q %v", text, spans)
	}
}

func TestHandwritingRejectsInvalidModelOutput(t *testing.T) {
	for _, raw := range []string{
		`{"pages":[]}`,
		`{"pages":[{"segments":[{"text":"猜测"}]}]}`,
		`{"pages":[{"segments":[{"text":"□","uncertain":false}]}]}`,
		`{"pages":[{"segments":[]},{"segments":[]}]}`,
		`{"pages":[{"segments":[{"text":"` + strings.Repeat("字", 20001) + `","uncertain":false}]}]}`,
	} {
		c := cloudai.NewMock()
		c.Set(RecognizeHandwriting.Name, raw)
		_, _, err := RecognizeHandwriting.Run(context.Background(), NewEngine(Config{Client: c}), 1, HandwritingIn{Images: []cloudai.Image{{Data: []byte("image")}}})
		if err == nil || len(c.Calls()) != 2 {
			t.Fatalf("不合格输出须重试一次并失败：calls=%d err=%v", len(c.Calls()), err)
		}
		if len(c.Calls()[1].Messages[1].Images) != 1 {
			t.Fatal("重试不能丢失图片")
		}
	}
}

type visionProviderError struct{}

func (visionProviderError) Complete(context.Context, cloudai.Request) (cloudai.Response, error) {
	return cloudai.Response{}, context.DeadlineExceeded
}

func TestHandwritingFallbackRetainsImages(t *testing.T) {
	c := cloudai.NewMock()
	c.Set(RecognizeHandwriting.Name, `{"pages":[{"segments":[]}]}`)
	e := NewEngine(Config{Client: visionProviderError{}, Fallback: &Fallback{Client: c, Models: Models{Strong: "vision-fallback"}}})
	_, meta, err := RecognizeHandwriting.Run(context.Background(), e, 1, HandwritingIn{Images: []cloudai.Image{{Data: []byte("private-image")}}})
	if err != nil {
		t.Fatal(err)
	}
	calls := c.Calls()
	if meta.Model != "vision-fallback" || len(calls) != 1 || len(calls[0].Messages[1].Images) != 1 || string(calls[0].Messages[1].Images[0].Data) != "private-image" {
		t.Fatal("备用平台调用应保留图片")
	}
}

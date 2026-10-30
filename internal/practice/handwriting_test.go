package practice_test

import (
	"context"
	"strings"
	"testing"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	cloudai "peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/cloud/moderation"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/practice"
)

func TestHandwritingUsesVisionModel(t *testing.T) {
	ctx := context.Background()
	c := cloudai.NewMock()
	c.Set(ai.RecognizeHandwriting.Name, `{"pages":[{"segments":[{"text":"意境😀","uncertain":false},{"text":"交融","uncertain":true}]},{"segments":[{"text":"□无穷","uncertain":true}]}]}`)
	o := oss.NewMock()
	// 不提供 OCR 依赖：拍照识别只能通过 AI 引擎调用视觉模型。
	s := practice.New(practice.Deps{AI: ai.NewEngine(ai.Config{Client: c}), OSS: o, Moderation: moderation.NewMock()})
	keys := []string{"u/42/hw/a.jpg", "u/42/hw/b.jpg"}
	o.Seed(keys[0], []byte("image-one"))
	o.Seed(keys[1], []byte("image-two"))
	out, err := s.Recognize(ctx, 42, keys)
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "意境😀交融\n□无穷" || len(out.Pages) != 2 || out.Pages[0].ObjectKey != keys[0] || len(out.LowConfidence) != 2 || out.LowConfidence[0] != [2]int{3, 5} || out.LowConfidence[1] != [2]int{6, 9} {
		t.Fatalf("识别结果：%+v", out)
	}
	calls := c.Calls()
	if len(calls) != 1 || len(calls[0].Messages[1].Images) != 2 {
		t.Fatalf("必须直接发送两张照片：%+v", calls)
	}
	if _, err = s.Recognize(ctx, 43, keys); kind(err) != apperr.NotFound || len(c.Calls()) != 1 {
		t.Fatalf("越权不能调用模型：%v", err)
	}
	c.Set(ai.RecognizeHandwriting.Name, `{"pages":[{"segments":[]}]}`)
	if _, err = s.Recognize(ctx, 42, keys[:1]); kind(err) != apperr.BadRequest {
		t.Fatalf("空白照片：%v", err)
	}
	o.Seed(keys[0], []byte(strings.Repeat("a", (10<<20)+1)))
	n := len(c.Calls())
	if _, err = s.Recognize(ctx, 42, keys[:1]); kind(err) != apperr.BadRequest || len(c.Calls()) != n {
		t.Fatalf("超大照片须在调用模型前拒绝：%v", err)
	}
}

// 即使单张都小于 10MB，多张 Base64 编码后也可能超过平台的请求体上限。
func TestHandwritingBatchesLargePhotos(t *testing.T) {
	c := cloudai.NewMock()
	c.Set(ai.RecognizeHandwriting.Name, `{"pages":[{"segments":[{"text":"答案","uncertain":false}]}]}`)
	o := oss.NewMock()
	s := practice.New(practice.Deps{AI: ai.NewEngine(ai.Config{Client: c}), OSS: o, Moderation: moderation.NewMock()})
	keys := []string{"u/42/hw/a.jpg", "u/42/hw/b.jpg"}
	data := []byte(strings.Repeat("x", 6<<20))
	for _, k := range keys {
		o.Seed(k, data)
	}
	out, err := s.Recognize(context.Background(), 42, keys)
	if err != nil || out.Text != "答案\n答案" {
		t.Fatalf("批次拼接：%+v %v", out, err)
	}
	if calls := c.Calls(); len(calls) != 2 || len(calls[0].Messages[1].Images) != 1 || len(calls[1].Messages[1].Images) != 1 {
		t.Fatalf("大照片应拆成两个请求，calls=%d", len(calls))
	}
	c.Set(ai.RecognizeHandwriting.Name, `{"pages":[{"segments":[{"text":"`+strings.Repeat("字", 10000)+`","uncertain":false}]}]}`)
	if _, err = s.Recognize(context.Background(), 42, keys); kind(err) != apperr.BadRequest {
		t.Fatalf("分批结果合计不得超过答案字数上限：%v", err)
	}
}

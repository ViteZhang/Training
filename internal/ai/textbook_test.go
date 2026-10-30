package ai

import (
	"context"
	cloudai "peetraining-server/internal/cloud/ai"
	"strings"
	"testing"
)

func TestTextbookVisionPageValidation(t *testing.T) {
	client := cloudai.NewMock()
	client.Set(RecognizeTextbook.Name, `{"page_no":7,"printed_page":"iv","blank":false,"blocks":[{"heading":"第一章","text":"忠实□原文","uncertain":true}]}`)
	out, meta, err := RecognizeTextbook.Run(context.Background(), NewEngine(Config{Client: client}), 0, TextbookPageIn{PageNo: 7, Image: cloudai.Image{Data: []byte("private-page")}})
	if err != nil || out.PageNo != 7 || meta.Model != "qwen3.8-flash" {
		t.Fatal(err)
	}
	call := client.Calls()[0]
	if len(call.Messages[1].Images) != 1 || strings.Contains(call.Messages[1].Content, "private-page") {
		t.Fatal("视觉页必须作为图片输入")
	}
	for _, bad := range []string{`{"page_no":8,"printed_page":"","blank":true,"blocks":[]}`, `{"page_no":7,"printed_page":"","blank":false,"blocks":[]}`, `{"page_no":7,"printed_page":"","blank":true,"blocks":[{"heading":"","text":"猜测","uncertain":false}]}`, `{"page_no":7,"printed_page":"","blank":false,"blocks":[{"heading":"","text":"□","uncertain":false}]}`} {
		client := cloudai.NewMock()
		client.Set(RecognizeTextbook.Name, bad)
		if _, _, err := RecognizeTextbook.Run(context.Background(), NewEngine(Config{Client: client}), 0, TextbookPageIn{PageNo: 7, Image: cloudai.Image{Data: []byte("page")}}); err == nil || len(client.Calls()) != 2 {
			t.Fatal("无效模型输出须校验重试并失败", err)
		}
	}
}

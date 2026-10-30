package ai

import (
	"errors"
	cloudai "peetraining-server/internal/cloud/ai"
	"strings"
	"unicode/utf8"
)

type TextbookPageIn struct {
	PageNo int           `json:"page_no"`
	Image  cloudai.Image `json:"-"`
}
type TextbookBlock struct {
	Heading   string `json:"heading"`
	Text      string `json:"text"`
	Uncertain bool   `json:"uncertain"`
}
type TextbookPageOut struct {
	PageNo      int             `json:"page_no"`
	PrintedPage string          `json:"printed_page"`
	Blank       bool            `json:"blank"`
	Blocks      []TextbookBlock `json:"blocks"`
}

var RecognizeTextbook = &Cap[TextbookPageIn, TextbookPageOut]{
	Def:    Def{Name: "textbook_recognize", Version: "v1", Tier: "strong", Temperature: 0, Schema: `{"type":"object","required":["page_no","printed_page","blank","blocks"],"additionalProperties":false,"properties":{"page_no":{"type":"integer","minimum":1},"printed_page":{"type":"string","maxLength":32},"blank":{"type":"boolean"},"blocks":{"type":"array","maxItems":1000,"items":{"type":"object","required":["heading","text","uncertain"],"additionalProperties":false,"properties":{"heading":{"type":"string","maxLength":512},"text":{"type":"string","minLength":1,"maxLength":20000},"uncertain":{"type":"boolean"}}}}}}`},
	Images: func(in TextbookPageIn) []cloudai.Image { return []cloudai.Image{in.Image} },
	Check: func(in TextbookPageIn, out *TextbookPageOut) error {
		if out.PageNo != in.PageNo || in.PageNo < 1 {
			return errors.New("页序不一致")
		}
		if out.Blank != (len(out.Blocks) == 0) {
			return errors.New("空白页标记与正文不一致")
		}
		n := 0
		for _, b := range out.Blocks {
			n += utf8.RuneCountInString(b.Text) + utf8.RuneCountInString(b.Heading)
			if strings.TrimSpace(b.Text) == "" || strings.ContainsRune(b.Text, '□') && !b.Uncertain {
				return errors.New("不可辨认内容必须标记不确定")
			}
		}
		if n > 20000 {
			return errors.New("单页内容过长")
		}
		return nil
	},
	Mock: func(in TextbookPageIn) (TextbookPageOut, error) {
		// mock 仅接受显式 UTF-8 测试页；真实图片不能伪造识别成功。
		if !utf8.Valid(in.Image.Data) {
			return TextbookPageOut{}, errors.New("真实教材图片需要启用视觉模型，mock 仅用于文本夹具")
		}
		text := strings.TrimSpace(string(in.Image.Data))
		out := TextbookPageOut{PageNo: in.PageNo, Blocks: []TextbookBlock{}, Blank: text == ""}
		if !out.Blank {
			out.Blocks = append(out.Blocks, TextbookBlock{Text: text, Uncertain: strings.ContainsAny(text, "□【")})
		}
		return out, nil
	},
}

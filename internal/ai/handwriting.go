package ai

import (
	"errors"
	"strings"
	"unicode/utf8"

	cloudai "peetraining-server/internal/cloud/ai"
)

// HandwritingIn 的图片仅作为视觉输入，不插入提示词中的 JSON。
type HandwritingIn struct {
	Images []cloudai.Image `json:"-"`
}

type HandwritingSegment struct {
	Text      string `json:"text"`
	Uncertain bool   `json:"uncertain"`
}

type HandwritingPage struct {
	Segments []HandwritingSegment `json:"segments"`
}

type HandwritingOut struct {
	Pages []HandwritingPage `json:"pages"`
}

// TextAndSpans 用服务端计算 Unicode 字符位置，避免模型猜测下标导致高亮错位。
func (p HandwritingPage) TextAndSpans() (string, [][2]int) {
	var text strings.Builder
	var spans [][2]int
	offset := 0
	for _, s := range p.Segments {
		n := utf8.RuneCountInString(s.Text)
		if s.Uncertain && n > 0 {
			spans = append(spans, [2]int{offset, offset + n})
		}
		text.WriteString(s.Text)
		offset += n
	}
	return text.String(), spans
}

var RecognizeHandwriting = &Cap[HandwritingIn, HandwritingOut]{
	Def: Def{Name: "handwriting_recognize", Version: "v1", Tier: "strong", Temperature: 0, Schema: `{
  "type":"object","required":["pages"],"additionalProperties":false,
  "properties":{"pages":{"type":"array","minItems":1,"maxItems":6,"items":{
   "type":"object","required":["segments"],"additionalProperties":false,
   "properties":{"segments":{"type":"array","maxItems":20000,"items":{
    "type":"object","required":["text","uncertain"],"additionalProperties":false,
    "properties":{"text":{"type":"string","maxLength":20000},"uncertain":{"type":"boolean"}}
   }}}
  }}}
 }`},
	Images: func(in HandwritingIn) []cloudai.Image { return in.Images },
	Check: func(in HandwritingIn, out *HandwritingOut) error {
		if len(out.Pages) != len(in.Images) {
			return errors.New("识别页数与照片数量不一致")
		}
		n := len(out.Pages) - 1 // 页之间的换行也计入答案长度上限。
		for _, p := range out.Pages {
			for _, s := range p.Segments {
				if strings.ContainsRune(s.Text, '□') && !s.Uncertain {
					return errors.New("无法辨认的字必须标为不确定")
				}
				n += utf8.RuneCountInString(s.Text)
			}
		}
		if n > 20000 {
			return errors.New("识别文字超过 20000 字")
		}
		return nil
	},
	Mock: mockHandwriting,
}

// mock 照片使用 UTF-8 文字夹具，【】仅表示不确定区域，不调用传统 OCR。
func mockHandwriting(in HandwritingIn) (HandwritingOut, error) {
	out := HandwritingOut{Pages: []HandwritingPage{}}
	for _, img := range in.Images {
		p := HandwritingPage{Segments: []HandwritingSegment{}}
		uncertain := false
		var text strings.Builder
		flush := func() {
			if text.Len() > 0 {
				p.Segments = append(p.Segments, HandwritingSegment{Text: text.String(), Uncertain: uncertain})
				text.Reset()
			}
		}
		for _, r := range strings.TrimSpace(string(img.Data)) {
			switch r {
			case '【':
				flush()
				uncertain = true
			case '】':
				flush()
				uncertain = false
			default:
				text.WriteRune(r)
			}
		}
		flush()
		out.Pages = append(out.Pages, p)
	}
	return out, nil
}

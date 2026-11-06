package official

import (
	"regexp"
	"strings"
)

// T52 后台导入的 Markdown：解析前去掉排版标记，只留文字。标题、题号、「第一组：…」这些行原样保留，
// 真题卷的题型、分值、限选分组仍由 importer 的规则识别。
var (
	mdFence     = regexp.MustCompile("^\\s*(```|~~~)")
	mdHeading   = regexp.MustCompile(`^\s{0,3}#{1,6}\s+`)
	mdHeadTail  = regexp.MustCompile(`\s+#+\s*$`)
	mdQuote     = regexp.MustCompile(`^\s{0,3}(>\s?)+`)
	mdBullet    = regexp.MustCompile(`^(\s*)[-*+]\s+(\[[ xX]\]\s+)?`)
	mdRule      = regexp.MustCompile(`^\s{0,3}([-*_])(\s*[-*_]){2,}\s*$`)
	mdTableSep  = regexp.MustCompile(`^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$`)
	mdImage     = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	mdLink      = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	mdBreak     = regexp.MustCompile(`(?i)<br\s*/?>`)
	mdTag       = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
	mdEmphasis  = regexp.MustCompile(`\*\*|__|~~`)
	mdItalic    = regexp.MustCompile(`(^|[^*\w])\*([^*\s][^*\n]*?)\*`)
	mdCode      = regexp.MustCompile("`+")
	mdEscape    = regexp.MustCompile(`\\([\\` + "`" + `*_{}\[\]()#+\-.!|>])`)
	mdSpaceRuns = regexp.MustCompile(`[ \t]{2,}`)
)

// markdownText 把 Markdown 转成纯文字：表格逐行、单元格之间用空格隔开；图片、链接网址、HTML 标签去掉。
func markdownText(md string) string {
	md = strings.TrimPrefix(md, "\ufeff")
	md = strings.ReplaceAll(md, "\r\n", "\n")
	lines := strings.Split(md, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if mdFence.MatchString(l) || mdRule.MatchString(l) || mdTableSep.MatchString(l) {
			continue
		}
		l = mdBreak.ReplaceAllString(l, "\n")
		l = mdTag.ReplaceAllString(l, "")
		l = mdQuote.ReplaceAllString(l, "")
		if mdHeading.MatchString(l) {
			l = mdHeadTail.ReplaceAllString(mdHeading.ReplaceAllString(l, ""), "")
		}
		l = mdBullet.ReplaceAllString(l, "$1")
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "|") || strings.Count(t, "|") >= 2 {
			cells := strings.Split(strings.Trim(t, "|"), "|")
			for i := range cells {
				cells[i] = strings.TrimSpace(cells[i])
			}
			l = strings.Join(cells, "  ")
		}
		l = mdImage.ReplaceAllString(l, "")
		l = mdLink.ReplaceAllString(l, "$1")
		l = mdEmphasis.ReplaceAllString(l, "")
		l = mdItalic.ReplaceAllString(l, "$1$2")
		l = mdCode.ReplaceAllString(l, "")
		l = mdEscape.ReplaceAllString(l, "$1")
		out = append(out, strings.TrimRight(mdSpaceRuns.ReplaceAllString(l, "  "), " \t"))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

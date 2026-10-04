// Package importer 是导入流水线（dev-spec 第六节）：建任务与预占额度、取文本、内容安全、切题、AI 结构化、
// 匹配答案、采分点、知识点标签、去重，结果写进 import_items 待确认；确认后入库。
package importer

import (
	"regexp"
	"strconv"
	"strings"

	"peetraining-server/internal/ai"
)

// Page 是一页原文。
type Page struct {
	No   int
	Text string
}

var (
	reSectionHead = regexp.MustCompile(`^\s*([一二三四五六七八九十]{1,3})\s*[、.．]\s*(\S.{0,40})$`)
	rePerScore    = regexp.MustCompile(`每(?:小)?题\s*(\d+(?:\.\d+)?)\s*分`)
	reYearTitle   = regexp.MustCompile(`((?:19|20)\d{2})\s*年`)
	reQNoDot      = regexp.MustCompile(`^\s*(\d{1,3})\s*[.．、]\s*\S`)
	reQNoParen    = regexp.MustCompile(`^\s*[（(]\s*(\d{1,3})\s*[)）]\s*\S`)
	reAnswerHead  = regexp.MustCompile(`^\s*[【\[]?\s*(?:参考)?答案(?:与解析|及解析|解析)?\s*[】\]]?\s*[:：]?\s*$`)
)

// sectionQType 按题型标题判断题型（「一、名词解释（每题 5 分）」）。
func sectionQType(title string) string {
	rules := []struct{ kw, qtype string }{
		{"名词解释", "term"}, {"概念解释", "term"},
		{"多项选择", "multi_choice"}, {"多选", "multi_choice"},
		{"单项选择", "single_choice"}, {"单选", "single_choice"}, {"选择", "single_choice"},
		{"判断", "true_false"}, {"填空", "fill_blank"},
		{"论述", "discussion"}, {"材料分析", "discussion"}, {"分析题", "discussion"},
		{"简答", "short_answer"}, {"简述", "short_answer"}, {"辨析", "short_answer"},
		{"作文", "essay"}, {"写作", "essay"},
		{"计算", "calculation"},
	}
	for _, r := range rules {
		if strings.Contains(title, r.kw) {
			return r.qtype
		}
	}
	return ""
}

// Split 是第 5 步「切题」：按题号与题型标题把原文切成候选题块；某页规则切不开时整页作一块交给模型。
//
// 识别：
//   - 试卷标题里的年份（「2024 年 xx 大学 xx 专业考研真题」），之后的题都记这一年
//   - 题型标题「一、名词解释（每题 5 分）」：题型与每题分值
//   - 题号「1.」「1、」；全文先出现哪种编号就以哪种为题号，另一种（如「(1)」）当作题内的小点
//   - 「参考答案」标题之后的块标为答案，按年份 + 题号去配对（第 7 步）
func Split(pages []Page) []ai.Block {
	var (
		blocks    []ai.Block
		cur       *ai.Block
		curText   strings.Builder
		section   string
		qtype     string
		score     float64
		year      int
		inAnswers bool
		style     *regexp.Regexp
	)
	flush := func() {
		if cur != nil {
			cur.Text = strings.TrimSpace(curText.String())
			if cur.Text != "" {
				blocks = append(blocks, *cur)
			}
		}
		cur = nil
		curText.Reset()
	}
	for _, p := range pages {
		before := len(blocks)
		var loose strings.Builder // 本页不属于任何题的文字
		for _, line := range strings.Split(p.Text, "\n") {
			trim := strings.TrimSpace(line)
			if trim == "" {
				continue
			}
			if m := reYearTitle.FindStringSubmatch(trim); m != nil && len([]rune(trim)) <= 60 &&
				(strings.Contains(trim, "真题") || strings.Contains(trim, "试题") || strings.Contains(trim, "试卷") || strings.Contains(trim, "考研")) {
				flush()
				year, _ = strconv.Atoi(m[1])
				inAnswers = strings.Contains(trim, "答案")
				section, qtype, score = "", "", 0
				continue
			}
			if reAnswerHead.MatchString(trim) {
				flush()
				inAnswers = true
				continue
			}
			if m := reSectionHead.FindStringSubmatch(trim); m != nil && !strings.ContainsAny(m[2], "。；;") {
				flush()
				section = trim
				if q := sectionQType(m[2]); q != "" || !inAnswers {
					qtype = q
				}
				score = 0
				if s := rePerScore.FindStringSubmatch(trim); s != nil {
					score, _ = strconv.ParseFloat(s[1], 64)
				}
				if strings.Contains(m[2], "答案") {
					inAnswers = true
				}
				continue
			}
			if style == nil {
				if reQNoDot.MatchString(trim) {
					style = reQNoDot
				} else if reQNoParen.MatchString(trim) {
					style = reQNoParen
				}
			}
			if style != nil {
				if m := style.FindStringSubmatch(trim); m != nil {
					flush()
					cur = &ai.Block{Page: p.No, Section: section, QType: qtype, Score: score, Year: year, No: m[1], Answer: inAnswers}
					curText.WriteString(trim)
					continue
				}
			}
			if cur != nil {
				curText.WriteString("\n")
				curText.WriteString(trim)
			} else {
				loose.WriteString(trim)
				loose.WriteString("\n")
			}
		}
		if len(blocks) == before && cur == nil && len([]rune(strings.TrimSpace(loose.String()))) >= 20 {
			blocks = append(blocks, ai.Block{Page: p.No, Section: section, QType: qtype, Year: year, Whole: true, Answer: inAnswers, Text: strings.TrimSpace(loose.String())})
		}
	}
	flush()
	for i := range blocks {
		blocks[i].ID = i + 1
	}
	return blocks
}

// Chunk 把块分批交给模型：每批不超过 maxChars 字（长上下文也要控制单次输出长度与失败重跑的代价）。
func Chunk(blocks []ai.Block, maxChars int) [][]ai.Block {
	var out [][]ai.Block
	var cur []ai.Block
	n := 0
	for _, b := range blocks {
		size := len([]rune(b.Text))
		if len(cur) > 0 && n+size > maxChars {
			out = append(out, cur)
			cur, n = nil, 0
		}
		cur = append(cur, b)
		n += size
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

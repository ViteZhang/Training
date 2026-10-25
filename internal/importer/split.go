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
	// 题型标题：「三、论述题」后面的括号说明可以很长（「本题共分七组，请按专业分组答题……」），只限制括号前的部分。
	reSectionHead = regexp.MustCompile(`^\s*([一二三四五六七八九十]{1,3})\s*[、.．]\s*(\S.{0,40}?(?:[（(].*)?)$`)
	rePerScore    = regexp.MustCompile(`每(?:小)?题\s*(\d+(?:\.\d+)?)\s*分`)
	reYearTitle   = regexp.MustCompile(`((?:19|20)\d{2})\s*年`)
	reYearLead    = regexp.MustCompile(`^((?:19|20)\d{2})\s*\S`) // 「2024 北师大 808 中国语言文学真题」：年份在行首、没有「年」字
	reQNoDot      = regexp.MustCompile(`^\s*(\d{1,3})\s*[.．、]\s*\S`)
	reQNoParen    = regexp.MustCompile(`^\s*[（(]\s*(\d{1,3})\s*[)）]\s*\S`)
	reAnswerHead  = regexp.MustCompile(`^\s*[【\[]?\s*(?:参考)?答案(?:与解析|及解析|解析)?\s*[】\]]?\s*[:：]?\s*$`)
	// 「第一组：请选择文艺学、比较文学与世界文学专业的考生作答」：论述题按专业分组，只答报考专业那一组。
	reGroupHead = regexp.MustCompile(`^\s*(第\s*[一二三四五六七八九十\d]{1,3}\s*组)\s*[：:．.、]?\s*(.*)$`)
	reGroupFor  = regexp.MustCompile(`(?:请|供)?(?:选择|报考)?(.+?)(?:专业|方向)?的?考生(?:作答|选答|选做|答题)`)
	// 资料常从 Markdown 转来，行首的标题、引用、列表符号与成对的加粗星号不是内容。
	reMDLead = regexp.MustCompile(`^(?:#{1,6}\s+|>\s?|[-*•]\s+)+`)
)

// passageMinRunes：题型标题下、第一道题之前的文字至少这么长才当作共用材料（短的多是标题换行或说明）。
// 分组标题下的文字不受限制：组内第一题之前只会是这一组的材料。
const passageMinRunes = 30

// cleanLine 去掉 Markdown 标记：行首 #、>、列表符号，两端的 ** 或 __。
func cleanLine(line string) string {
	t := strings.TrimSpace(line)
	for {
		u := strings.TrimSpace(reMDLead.ReplaceAllString(t, ""))
		u = strings.TrimSpace(strings.Trim(u, "*_"))
		if u == t {
			return t
		}
		t = u
	}
}

// groupMajors 从分组说明里取出适用专业，如「请选择文艺学、比较文学与世界文学专业的考生作答」→「文艺学、比较文学与世界文学」。
func groupMajors(desc string) string {
	if m := reGroupFor.FindStringSubmatch(desc); m != nil {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(desc)
}

// isGroupHead：分组标题要么很短（「第一组」），要么写明给哪些考生作答；避免把题干里的「第二组数据……」当成分组。
func isGroupHead(line string) bool {
	n := len([]rune(line))
	if n <= 8 {
		return true
	}
	if n > 80 {
		return false
	}
	for _, kw := range []string{"考生", "专业", "方向", "作答", "选答", "选做"} {
		if strings.Contains(line, kw) {
			return true
		}
	}
	return false
}

// unclosedParen 判断题型标题里的括号是否还没闭合（标题折到了下一行）。
func unclosedParen(s string) bool {
	return strings.Count(s, "（")+strings.Count(s, "(") > strings.Count(s, "）")+strings.Count(s, ")")
}

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
//   - 分组标题「第一组：请选择××专业的考生作答」：之后的题记上组名与适用专业，直到下一个分组或题型标题
//   - 共用材料：分组标题（或题型标题）之后、第一道题之前的文字，挂到这一组（这一题型）后面的每道题上
//   - Markdown 转来的资料：先去掉行首 #、>、列表符号与加粗星号
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
		group     string
		majors    string
		passage   string          // 当前生效的共用材料
		pending   strings.Builder // 标题之后、第一道题之前收集到的文字
		inGroup   bool            // pending 是不是在分组标题下收集的
		openHead  bool            // 题型标题的括号还没闭合，下一行接到标题上
	)
	// resetContext 在新的题型、分组或试卷开始时清掉材料；不是新分组时连分组一起清掉。
	resetContext := func(newGroup bool) {
		pending.Reset()
		passage = ""
		inGroup = newGroup
		if !newGroup {
			group, majors = "", ""
		}
	}
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
			trim := cleanLine(line)
			if trim == "" {
				continue
			}
			if openHead {
				section += trim
				openHead = unclosedParen(section)
				if s := rePerScore.FindStringSubmatch(section); s != nil && score == 0 {
					score, _ = strconv.ParseFloat(s[1], 64)
				}
				continue
			}
			m := reYearTitle.FindStringSubmatch(trim)
			if m == nil {
				m = reYearLead.FindStringSubmatch(trim)
			}
			if m != nil && len([]rune(trim)) <= 60 &&
				(strings.Contains(trim, "真题") || strings.Contains(trim, "试题") || strings.Contains(trim, "试卷") || strings.Contains(trim, "考研")) {
				flush()
				year, _ = strconv.Atoi(m[1])
				inAnswers = strings.Contains(trim, "答案")
				section, qtype, score = "", "", 0
				resetContext(false)
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
				openHead = unclosedParen(trim)
				resetContext(false)
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
			if m := reGroupHead.FindStringSubmatch(trim); m != nil && !inAnswers && isGroupHead(trim) {
				flush()
				resetContext(true)
				group = strings.Join(strings.Fields(m[1]), "")
				majors = groupMajors(m[2])
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
					if t := strings.TrimSpace(pending.String()); t != "" && (inGroup || len([]rune(t)) >= passageMinRunes) {
						passage = t
					}
					pending.Reset()
					cur = &ai.Block{Page: p.No, Section: section, QType: qtype, Score: score, Year: year, No: m[1], Answer: inAnswers,
						Group: group, GroupMajors: majors, Passage: passage}
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
				if !inAnswers {
					pending.WriteString(trim)
					pending.WriteString("\n")
				}
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

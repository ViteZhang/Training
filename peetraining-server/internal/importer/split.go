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
	// 每题分值：「每题 10 分」，也有写成「10 分每题」的（川大等院校的试卷）。
	rePerScore = regexp.MustCompile(`每(?:小)?题\s*(\d+(?:\.\d+)?)\s*分|(\d+(?:\.\d+)?)\s*分\s*每(?:小)?题`)
	reAnyScore = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*分`)
	// 题末的分值「(25分)」
	reTrailScore = regexp.MustCompile(`[（(]\s*(\d+(?:\.\d+)?)\s*分\s*[)）]\s*$`)
	// 汉字题号的整行：「一、简述孔子的“为政以德”思想。(25分)」，标题太长 reSectionHead 匹配不上时也要认出来。
	reCNNumbered = regexp.MustCompile(`^\s*([一二三四五六七八九十]{1,3})\s*[、.．]\s*(\S.*)$`)
	// 「【中西哲学史】」：一张卷子里分科目的大部分标题。
	rePartHead   = regexp.MustCompile(`^[【\[]\s*([^】\]]{1,20}?)\s*[】\]]$`)
	reYearTitle  = regexp.MustCompile(`((?:19|20)\d{2})\s*年`)
	reYearLead   = regexp.MustCompile(`^((?:19|20)\d{2})\s*\S`) // 「2024 北师大 808 中国语言文学真题」：年份在行首、没有「年」字
	reQNoDot     = regexp.MustCompile(`^\s*(\d{1,3})\s*[.．、]\s*\S`)
	reQNoParen   = regexp.MustCompile(`^\s*[（(]\s*(\d{1,3})\s*[)）]\s*\S`)
	reAnswerHead = regexp.MustCompile(`^\s*[【\[]?\s*(?:参考)?答案(?:与解析|及解析|解析)?\s*[】\]]?\s*[:：]?\s*$`)
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

// perScore 取「每题 N 分」「N 分每题」里的分值。
func perScore(s string) float64 {
	m := rePerScore.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	v := m[1]
	if v == "" {
		v = m[2]
	}
	f, _ := strconv.ParseFloat(v, 64)
	return f
}

// beforeParen 取括号说明之前的部分：「一、名词解释(10 分每题，每个不超过 100 字。)」→「名词解释」。
func beforeParen(s string) string {
	if i := strings.IndexAny(s, "（("); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// questionLike 判断汉字编号后面的文字是一道题而不是题型标题：题型标题短且不成句（「名词解释」「简答题」「外国哲学史部分」），
// 题目是完整的句子（「简述孔子的“为政以德”思想。」）。括号里的说明（分值、字数要求）不参与判断。
func questionLike(title string) bool {
	head := beforeParen(reTrailScore.ReplaceAllString(title, ""))
	if strings.ContainsAny(head, "。？?；;") {
		return true
	}
	return len([]rune(head)) >= 10 && !strings.Contains(head, "题") && !strings.Contains(head, "部分")
}

// isSectionHead 判断一行是不是题型标题，返回标题里题号之后的文字。
func isSectionHead(line string) (string, bool) {
	m := reSectionHead.FindStringSubmatch(line)
	if m == nil || questionLike(m[2]) {
		return "", false
	}
	return m[2], true
}

// cnNumber 把一到九十九的汉字数字转成阿拉伯数字串：「三」→「3」，「十二」→「12」，「二十」→「20」。
func cnNumber(s string) string {
	digits := map[rune]int{'一': 1, '二': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	r := []rune(s)
	n := 0
	switch {
	case len(r) == 1 && r[0] == '十':
		n = 10
	case len(r) == 1:
		n = digits[r[0]]
	case len(r) == 2 && r[0] == '十':
		n = 10 + digits[r[1]]
	case len(r) == 2 && r[1] == '十':
		n = digits[r[0]] * 10
	case len(r) == 3 && r[1] == '十':
		n = digits[r[0]]*10 + digits[r[2]]
	}
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
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
//   - 汉字题号的题「一、简述……。(25分)」：一行就是一道题（与题型标题同级），题末括号是分值
//   - 大部分标题「【中西哲学史】」：结束上一部分的题型、分组与材料
//   - 题型标题下没有编号的唯一一道题（「三、论述题(50 分)」下直接是题干）：整段作为一道题，不当材料
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
		sectionNo string          // 当前题型标题的汉字序号（阿拉伯数字）
		total     float64         // 题型标题里的总分（没有每题分值时，给唯一一道题用）
		sectionQ  bool            // 当前题型下是否已经切出了题
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
	// closeSection：题型标题下没有编号的题时，标题之后收集的文字就是唯一的一道题（「三、论述题(50 分)」）。
	// 不收的话它会被当成共用材料挂到下一部分的题上，或者整道题丢掉。
	closeSection := func(pageNo int) {
		t := strings.TrimSpace(pending.String())
		if section != "" && !sectionQ && !inAnswers && group == "" && len([]rune(t)) >= 10 {
			sc := score
			if sc == 0 {
				sc = total
			}
			blocks = append(blocks, ai.Block{Page: pageNo, Section: section, QType: qtype, Score: sc, Year: year, No: sectionNo, Text: t})
		}
		section, sectionNo, total, sectionQ = "", "", 0, false
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
				if score == 0 {
					score = perScore(section)
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
				closeSection(p.No)
				year, _ = strconv.Atoi(m[1])
				inAnswers = strings.Contains(trim, "答案")
				qtype, score = "", 0
				resetContext(false)
				continue
			}
			if reAnswerHead.MatchString(trim) {
				flush()
				closeSection(p.No)
				inAnswers = true
				continue
			}
			if rePartHead.MatchString(trim) && !inAnswers {
				flush()
				closeSection(p.No)
				qtype, score = "", 0
				resetContext(false)
				continue
			}
			if title, ok := isSectionHead(trim); ok {
				flush()
				closeSection(p.No)
				section = trim
				sectionNo = cnNumber(reSectionHead.FindStringSubmatch(trim)[1])
				openHead = unclosedParen(trim)
				resetContext(false)
				if q := sectionQType(title); q != "" || !inAnswers {
					qtype = q
				}
				score = perScore(trim)
				if score == 0 {
					if s := reAnyScore.FindStringSubmatch(trim); s != nil {
						total, _ = strconv.ParseFloat(s[1], 64)
					}
				}
				if strings.Contains(title, "答案") {
					inAnswers = true
				}
				continue
			}
			// 汉字题号的题与题型标题同级：自成一块，并结束上一个题型。
			if m := reCNNumbered.FindStringSubmatch(trim); m != nil && !inAnswers && cnNumber(m[1]) != "" {
				flush()
				closeSection(p.No)
				qtype, score = "", 0
				resetContext(false)
				blk := &ai.Block{Page: p.No, QType: sectionQType(beforeParen(m[2])), Year: year, No: cnNumber(m[1])}
				if s := reTrailScore.FindStringSubmatch(trim); s != nil {
					blk.Score, _ = strconv.ParseFloat(s[1], 64)
				}
				cur = blk
				curText.WriteString(trim)
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
					sectionQ = true
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
		// 题型下还没有题、文字等着 closeSection 收成一道题时，不再整页兜底，免得同一道题出现两次。
		waiting := section != "" && !sectionQ && !inAnswers && group == "" && strings.TrimSpace(pending.String()) != ""
		if len(blocks) == before && cur == nil && !waiting && len([]rune(strings.TrimSpace(loose.String()))) >= 20 {
			blocks = append(blocks, ai.Block{Page: p.No, Section: section, QType: qtype, Year: year, Whole: true, Answer: inAnswers, Text: strings.TrimSpace(loose.String())})
		}
	}
	flush()
	closeSection(lastPage(pages))
	for i := range blocks {
		blocks[i].ID = i + 1
	}
	return blocks
}

func lastPage(pages []Page) int {
	if len(pages) == 0 {
		return 0
	}
	return pages[len(pages)-1].No
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

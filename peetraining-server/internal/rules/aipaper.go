package rules

import "sort"

// PaperKind 是 AI 组卷的类型（PRD 11.10）。
type PaperKind string

const (
	PaperStandard PaperKind = "ai_standard" // 标准卷
	PaperTargeted PaperKind = "ai_targeted" // 针对卷
)

// PaperSlot 是试卷结构里的一个题型：题型数量与分值等于用户最近一套真题卷。
type PaperSlot struct {
	QType     QType
	Count     int
	ScoreEach float64
}

// PaperCandidate 是一道可以出现在 AI 组卷里的题。
type PaperCandidate struct {
	QuestionID int64
	KPID       int64
	SectionID  int64
	QType      QType
	// ExamKP 表示主知识点在用户真题中考过。
	ExamKP bool
	M      float64
	// DoneInPaper 表示这是已在整卷中做过的真题，两种卷都不再出现。
	DoneInPaper bool
	// DaysSinceDone 是距上次做这道题的天数；从没做过为 -1。针对卷避开近 30 天做过的题。
	DaysSinceDone int
}

// ComposeInput 是组卷输入。SectionShares 是各板块的真题分值占比（用于按板块分配知识点）。
type ComposeInput struct {
	Kind          PaperKind
	Slots         []PaperSlot
	Candidates    []PaperCandidate
	SectionShares map[int64]float64
}

// ComposedItem 是组出的一道题。
type ComposedItem struct {
	Seq        int
	QuestionID int64
	QType      QType
	Score      float64
}

// ComposeResult 是组卷结果。Shortfall 是各题型缺的题数，由调用方用 AI 变式题补足并逐题标「AI 出题」
// （变式题只能基于用户确认过的知识点和采分点生成）。
type ComposeResult struct {
	Items     []ComposedItem
	Shortfall map[QType]int
}

// ComposePaper 按用户真题的题型结构从题库抽题（PRD 11.10）：
//
//   - 标准卷：知识点按板块真题分值占比分配；真题考过的知识点占 50–70%（取中值为目标）；同一知识点不重复；
//     不出现已在整卷中做过的真题
//   - 针对卷：结构同标准卷，薄弱知识点（M < 60）占比提高到 50%；避开近 30 天做过的题
//
// 每次挑选时给候选打分：板块缺口 + 是否符合考过 / 薄弱的目标 + 低掌握度微调，分数相同取题目 ID 小的，结果确定可复现。
func ComposePaper(in ComposeInput, p AIPaperParams) ComposeResult {
	res := ComposeResult{Shortfall: map[QType]int{}}
	total := 0
	for _, s := range in.Slots {
		total += s.Count
	}
	examTarget := int(float64(total)*(p.ExamKPShareMin+p.ExamKPShareMax)/2 + 0.5)
	weakTarget := 0
	if in.Kind == PaperTargeted {
		weakTarget = int(float64(total)*p.TargetedWeakShare + 0.5)
	}

	cands := append([]PaperCandidate{}, in.Candidates...)
	sort.Slice(cands, func(i, j int) bool { return cands[i].QuestionID < cands[j].QuestionID })

	usedKP := map[int64]bool{}
	usedQ := map[int64]bool{}
	sectionPicked := map[int64]int{}
	picked, examPicked, weakPicked := 0, 0, 0
	seq := 0
	for _, slot := range in.Slots {
		for range slot.Count {
			best, bestScore := -1, 0.0
			for i, c := range cands {
				if !eligible(c, slot.QType, in.Kind, usedKP, usedQ, p) {
					continue
				}
				score := sectionDeficit(c.SectionID, in.SectionShares, sectionPicked, picked+1)
				if (examPicked < examTarget) == c.ExamKP {
					score++
				}
				if in.Kind == PaperTargeted && (weakPicked < weakTarget) == (c.M < p.WeakMaxM) {
					score++
				}
				score += (100 - clamp(c.M, 0, 100)) / 1000
				if best < 0 || score > bestScore {
					best, bestScore = i, score
				}
			}
			seq++
			if best < 0 {
				res.Shortfall[slot.QType]++
				continue
			}
			c := cands[best]
			usedKP[c.KPID], usedQ[c.QuestionID] = true, true
			sectionPicked[c.SectionID]++
			picked++
			if c.ExamKP {
				examPicked++
			}
			if c.M < p.WeakMaxM {
				weakPicked++
			}
			res.Items = append(res.Items, ComposedItem{Seq: seq, QuestionID: c.QuestionID, QType: slot.QType, Score: slot.ScoreEach})
		}
	}
	return res
}

func eligible(c PaperCandidate, q QType, kind PaperKind, usedKP, usedQ map[int64]bool, p AIPaperParams) bool {
	if c.QType != q || c.DoneInPaper || usedKP[c.KPID] || usedQ[c.QuestionID] {
		return false
	}
	if kind == PaperTargeted && c.DaysSinceDone >= 0 && c.DaysSinceDone < p.TargetedAvoidDays {
		return false
	}
	return true
}

// sectionDeficit 是板块按分值占比「应得题数 − 已选题数」，缺得越多越优先；没有占比数据时各板块等权。
func sectionDeficit(section int64, shares map[int64]float64, picked map[int64]int, n int) float64 {
	share := 0.0
	if len(shares) == 0 {
		share = 1
	} else {
		share = shares[section]
	}
	return share*float64(n) - float64(picked[section])
}

package rules

import (
	"fmt"
	"sort"
	"strings"
)

// ExamQuestion 是一道来源为「真题」的题。
type ExamQuestion struct {
	Year         int
	QType        QType
	Score        float64 // 0 表示缺分值
	Recollection bool    // 回忆版
	SectionID    int64
	KPIDs        []int64
}

// StructureSlot 是题型结构里的一项。
type StructureSlot struct {
	QType     QType
	Count     int
	ScoreEach float64
	Total     float64
}

// KPCount 是知识点的真题出现次数。
type KPCount struct {
	KPID  int64
	Count int
}

// ExamProfile 是考情分析统计结果（PRD 11.11）。
type ExamProfile struct {
	// Ready 为 false 时（少于 2 套真题卷）只提示再导入，不显示统计。
	Ready      bool
	PaperCount int
	Years      []int
	// Excluded 是回忆版、缺分值等不计入的题数，界面注明。
	Excluded  int
	Structure []StructureSlot
	// StableYears 是最近连续几年结构未变（「近 N 年未变」）；ChangedYears 是近 3 年里结构不同的年份。
	StableYears   int
	ChangedYears  []int
	SectionShares map[int64]float64
	HighFreq      []KPCount
}

// BuildExamProfile 统计考情（PRD 11.11）：
//
//   - 只统计来源为「真题」且有年份、有分值的题；回忆版、缺分值的题不计入并注明数量
//   - 至少 2 套真题卷（不同年份）才显示统计
//   - 题型结构取最近 3 年出现最多的结构（同样多时取较近的），并注明「近 N 年未变」或变化年份
//   - 板块分值占比 = 各板块真题分值之和 / 全部真题分值
//   - 高频考点 = 真题出现 ≥ 2 次的知识点，按次数排序
func BuildExamProfile(qs []ExamQuestion, p ExamProfileParams) ExamProfile {
	var prof ExamProfile
	byYear := map[int][]ExamQuestion{}
	sectionScore := map[int64]float64{}
	kpCount := map[int64]int{}
	total := 0.0
	for _, q := range qs {
		if q.Year <= 0 || q.Score <= 0 || q.Recollection {
			prof.Excluded++
			continue
		}
		byYear[q.Year] = append(byYear[q.Year], q)
		sectionScore[q.SectionID] += q.Score
		total += q.Score
		for _, kp := range q.KPIDs {
			kpCount[kp]++
		}
	}
	for y := range byYear {
		prof.Years = append(prof.Years, y)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(prof.Years)))
	prof.PaperCount = len(prof.Years)
	if prof.PaperCount < p.MinPapers {
		return prof
	}
	prof.Ready = true

	recent := prof.Years
	if len(recent) > p.StructureRecentYears {
		recent = recent[:p.StructureRecentYears]
	}
	sigs := map[int]string{}
	structs := map[string][]StructureSlot{}
	freq := map[string]int{}
	for _, y := range prof.Years {
		s := structureOf(byYear[y])
		sig := signature(s)
		sigs[y] = sig
		structs[sig] = s
	}
	best := ""
	for _, y := range recent {
		freq[sigs[y]]++
		if best == "" || freq[sigs[y]] > freq[best] {
			best = sigs[y]
		}
	}
	prof.Structure = structs[best]
	for _, y := range prof.Years {
		if sigs[y] != best {
			break
		}
		prof.StableYears++
	}
	for _, y := range recent {
		if sigs[y] != best {
			prof.ChangedYears = append(prof.ChangedYears, y)
		}
	}

	prof.SectionShares = map[int64]float64{}
	for s, v := range sectionScore {
		prof.SectionShares[s] = v / total
	}
	for kp, n := range kpCount {
		if n >= p.HighFreqMinCount {
			prof.HighFreq = append(prof.HighFreq, KPCount{KPID: kp, Count: n})
		}
	}
	sort.Slice(prof.HighFreq, func(i, j int) bool {
		if prof.HighFreq[i].Count != prof.HighFreq[j].Count {
			return prof.HighFreq[i].Count > prof.HighFreq[j].Count
		}
		return prof.HighFreq[i].KPID < prof.HighFreq[j].KPID
	})
	return prof
}

// qtypeOrder 让结构按「名词解释 → 简答 → 论述 → 作文 → 其他」排列，与试卷常见顺序一致。
var qtypeOrder = map[QType]int{QTermExplain: 1, QShortAnswer: 2, QDiscussion: 3, QEssay: 4}

func structureOf(qs []ExamQuestion) []StructureSlot {
	m := map[QType]*StructureSlot{}
	for _, q := range qs {
		s, ok := m[q.QType]
		if !ok {
			s = &StructureSlot{QType: q.QType}
			m[q.QType] = s
		}
		s.Count++
		s.Total += q.Score
	}
	out := make([]StructureSlot, 0, len(m))
	for _, s := range m {
		s.ScoreEach = s.Total / float64(s.Count)
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		oi, oj := qtypeOrder[out[i].QType], qtypeOrder[out[j].QType]
		if oi == 0 {
			oi = 99
		}
		if oj == 0 {
			oj = 99
		}
		if oi != oj {
			return oi < oj
		}
		return out[i].QType < out[j].QType
	})
	return out
}

func signature(s []StructureSlot) string {
	parts := make([]string, len(s))
	for i, x := range s {
		parts[i] = fmt.Sprintf("%s:%d:%g", x.QType, x.Count, x.Total)
	}
	return strings.Join(parts, "|")
}

// SectionCoverage 是判断「缺资料提醒」需要的板块数据。
type SectionCoverage struct {
	SectionID int64
	Share     float64 // 真题分值占比
	KPCount   int
	Mastery   float64 // 板块掌握度 0–100
}

// MissingMaterialSections 找出需要提醒补资料的板块（PRD 11.11）：
// 板块分值占比 ≥ 10%，且该板块知识点数不到各板块平均值的一半，或板块掌握度 < 20。
func MissingMaterialSections(secs []SectionCoverage, p ExamProfileParams) []int64 {
	if len(secs) == 0 {
		return nil
	}
	sum := 0
	for _, s := range secs {
		sum += s.KPCount
	}
	avg := float64(sum) / float64(len(secs))
	var out []int64
	for _, s := range secs {
		if s.Share < p.MissingMinShare {
			continue
		}
		if float64(s.KPCount) < avg*p.MissingKPRatio || s.Mastery < p.MissingMaxMastery {
			out = append(out, s.SectionID)
		}
	}
	return out
}

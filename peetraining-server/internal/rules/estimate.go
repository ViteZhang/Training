package rules

import (
	"math"
	"sort"
	"strconv"
)

// PaperScore 是一套做完的导入真题卷成绩，已换算到整卷满分（见 ScalePaperScore）。
type PaperScore struct {
	Score float64
}

// QTypeStat 是某题型用于模型分的数据。
type QTypeStat struct {
	QType QType
	// TotalInPaper 是该题型在真题卷中的总分值（取最近一套真题卷的结构）。
	TotalInPaper float64
	// RecentRates 是该题型近期每道题的得分率，新的在前；最多取前 QTypeRecentQuestions 道。
	RecentRates []float64
	// LastPaperRate 是最近一套整卷中该题型的得分率；近期作答不足 5 题时用它。
	LastPaperRate float64
}

// EstimateInput 是预估分的输入。Papers 只包含做完的导入真题卷，新的在前；AI 组卷不计入。
type EstimateInput struct {
	FullScore             float64
	Papers                []PaperScore
	QTypes                []QTypeStat
	RecentSubjectiveCount int // 卡片上「近多少道主观题」
}

// Estimate 是预估分结果。
type Estimate struct {
	Low, High      int
	Mid            float64
	Measured       float64
	Model          float64
	BasisPapers    int
	BasisQuestions int
	// MainGap 是「主要差在」的题型：题型分值 ×（1 − 得分率）最大的那个。
	MainGap QType
}

// ScalePaperScore 把缺题卷的得分换算到整卷满分（PRD 11.6）：按实际题目的满分计分，再换算。
func ScalePaperScore(got, actualFull, paperFull float64) float64 {
	if actualFull <= 0 {
		return 0
	}
	return got / actualFull * paperFull
}

// EstimateScore 计算预估分（PRD 11.6）。至少做完一套导入真题卷才有结果（ok=false 时不显示预估分）。
//
//	实测分 = 最近 2 套导入真题卷的整卷得分平均（只做过 1 套时取该套）
//	模型分 = Σ 题型在真题卷中的总分值 × 该题型近 20 题得分率（不足 5 题用最近一套整卷中该题型的得分率）
//	预估分中值 = 0.6 × 实测分 + 0.4 × 模型分
//	区间：依据 1 套卷 ±10%，2 套 ±6%，3 套及以上 ±4%（以中值为基数，见 open-questions Q13），截断在 0–满分
func EstimateScore(in EstimateInput, p ScoreEstimateParams) (Estimate, bool) {
	if len(in.Papers) == 0 {
		return Estimate{}, false
	}
	n := min(len(in.Papers), p.MeasuredRecentPapers)
	measured := 0.0
	for _, ps := range in.Papers[:n] {
		measured += ps.Score
	}
	measured /= float64(n)

	model := 0.0
	mainGap, maxLoss := QType(""), -1.0
	for _, qs := range in.QTypes {
		rate := qtypeRate(qs, p)
		model += qs.TotalInPaper * rate
		if loss := qs.TotalInPaper * (1 - rate); loss > maxLoss {
			mainGap, maxLoss = qs.QType, loss
		}
	}

	mid := p.MeasuredWeight*measured + p.ModelWeight*model
	width := widthFor(len(in.Papers), p.Width)
	low := clamp(math.Round(mid*(1-width)), 0, in.FullScore)
	high := clamp(math.Round(mid*(1+width)), 0, in.FullScore)
	return Estimate{
		Low: int(low), High: int(high), Mid: mid, Measured: measured, Model: model,
		BasisPapers: len(in.Papers), BasisQuestions: in.RecentSubjectiveCount, MainGap: mainGap,
	}, true
}

func qtypeRate(qs QTypeStat, p ScoreEstimateParams) float64 {
	rates := qs.RecentRates
	if len(rates) > p.QTypeRecentQuestions {
		rates = rates[:p.QTypeRecentQuestions]
	}
	if len(rates) < p.QTypeMinQuestions {
		return clamp(qs.LastPaperRate, 0, 1)
	}
	sum := 0.0
	for _, r := range rates {
		sum += clamp(r, 0, 1)
	}
	return sum / float64(len(rates))
}

// widthFor 取依据 n 套卷对应的区间宽度；超过表里最大套数时用最大那档。
func widthFor(n int, widths map[string]float64) float64 {
	keys := make([]int, 0, len(widths))
	for k := range widths {
		if v, err := strconv.Atoi(k); err == nil {
			keys = append(keys, v)
		}
	}
	sort.Ints(keys)
	w := 0.0
	for _, k := range keys {
		if k <= n {
			w = widths[strconv.Itoa(k)]
		}
	}
	return w
}

// EstimateEssay 计算作文课预估分（PRD 11.13）：最近 3 篇「按用户评分细则批改、且以真题限时完成」的作文得分平均；
// 区间宽度按篇数同 11.6。scores 新的在前，只包含计入的作文；一篇都没有时不显示预估分。
func EstimateEssay(scores []float64, fullScore float64, p ScoreEstimateParams) (Estimate, bool) {
	if len(scores) == 0 {
		return Estimate{}, false
	}
	n := min(len(scores), max(p.EssayRecent, 1))
	mid := 0.0
	for _, s := range scores[:n] {
		mid += s
	}
	mid /= float64(n)
	width := widthFor(n, p.Width)
	return Estimate{
		Low: int(clamp(math.Round(mid*(1-width)), 0, fullScore)), High: int(clamp(math.Round(mid*(1+width)), 0, fullScore)),
		Mid: mid, Measured: mid, BasisPapers: n,
	}, true
}

// DimScore 是作文一个评分维度的得分。
type DimScore struct {
	Name       string
	Score, Max float64
}

// WeakestDimension 是失分主项：得分率最低的维度（PRD 11.13），并列时取靠前的。
func WeakestDimension(dims []DimScore) (string, bool) {
	best, rate := "", 2.0
	for _, d := range dims {
		if d.Max <= 0 {
			continue
		}
		if r := d.Score / d.Max; r < rate {
			best, rate = d.Name, r
		}
	}
	return best, best != ""
}

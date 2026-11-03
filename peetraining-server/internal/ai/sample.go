package ai

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// SampleQuestionIn 是 AI 出体验题的输入（PRD v3 增补「AI 出题（通用体验题）」）：
// 已覆盖学科按方向批量生成初稿、团队审核后上线；未覆盖学科按用户的专业课和参考教材现场生成一道。
type SampleQuestionIn struct {
	Subject   string   `json:"subject"`   // 专业课名称或方向，如「西方哲学史」「古代文学」
	Textbooks []string `json:"textbooks"` // 参考教材，可空
	QType     string   `json:"qtype"`     // term / short_answer / discussion
	Score     float64  `json:"score"`
	Avoid     []string `json:"avoid,omitempty"` // 已有的题干，避免重复
}

type SampleQuestionOut struct {
	Stem   string        `json:"stem"`
	Answer string        `json:"answer"`
	Points []RubricPoint `json:"points"`
}

// SampleQuestion 出一道体验题：题干、参考答案与采分点。采分点合计必须等于分值（差额由校验按比例修正前先判断是否离谱）。
var SampleQuestion = &Cap[SampleQuestionIn, SampleQuestionOut]{
	Def: Def{Name: "sample_question", Version: "v1", Tier: "cheap", Temperature: 0.6, Schema: `{
		"type": "object", "required": ["stem", "answer", "points"],
		"properties": {
			"stem": {"type": "string", "minLength": 2, "maxLength": 300},
			"answer": {"type": "string", "minLength": 10},
			"points": ` + rubricPoints + `
		}}`},
	Check: func(in SampleQuestionIn, out *SampleQuestionOut) error {
		stem := strings.TrimSpace(out.Stem)
		for _, a := range in.Avoid {
			if compact(a) == compact(stem) {
				return errors.New("和已有的题重复")
			}
		}
		total := 0.0
		for _, p := range out.Points {
			total += p.Score
		}
		if total <= 0 {
			return errors.New("采分点没有分值")
		}
		// 合计差得太多说明模型没按分值出题；差一点按比例修正到分值，保证「采分点合计等于题目分值」。
		if math.Abs(total-in.Score) > in.Score*0.3 {
			return fmt.Errorf("采分点合计 %.1f 与分值 %.1f 相差太多", total, in.Score)
		}
		ScalePoints(out.Points, in.Score)
		return nil
	},
	Mock: func(in SampleQuestionIn) (SampleQuestionOut, error) {
		stem := "名词解释：" + in.Subject + "的核心概念"
		switch in.QType {
		case "short_answer":
			stem = "简述" + in.Subject + "的主要内容"
		case "discussion":
			stem = "论述" + in.Subject + "的意义"
		}
		for i := 2; contains(in.Avoid, stem); i++ {
			stem = fmt.Sprintf("%s（%d）", stem, i)
		}
		ans := in.Subject + "的核心概念是指研究对象的基本规定。它强调概念的内涵与外延。它在学科体系中具有基础地位。"
		return SampleQuestionOut{Stem: stem, Answer: ans, Points: splitPoints(ans, in.Score)}, nil
	},
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// ScalePoints 把采分点分值按比例调到合计等于 total，按 0.5 取整，余差记在最后一个采分点上。
func ScalePoints(ps []RubricPoint, total float64) {
	sum := 0.0
	for _, p := range ps {
		sum += p.Score
	}
	if sum <= 0 || len(ps) == 0 || math.Abs(sum-total) < 1e-9 {
		return
	}
	acc := 0.0
	for i := range ps {
		if i == len(ps)-1 {
			ps[i].Score = math.Round((total-acc)*2) / 2
			break
		}
		ps[i].Score = math.Max(0.5, math.Round(ps[i].Score/sum*total*2)/2)
		acc += ps[i].Score
	}
}

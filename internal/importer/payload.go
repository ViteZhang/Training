package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/dbtypes"
)

// QuestionDraft 是待确认题目（import_items.payload），JSON 与契约里的 QuestionDraft 一致，多出的 source_page 只在服务端用。
type QuestionDraft struct {
	QType          string           `json:"qtype"`
	Stem           string           `json:"stem"`
	Options        []ai.Option      `json:"options,omitempty"`
	Answer         string           `json:"answer,omitempty"`
	AnswerOrigin   string           `json:"answer_origin,omitempty"`
	Analysis       string           `json:"analysis,omitempty"`
	Score          *float64         `json:"score,omitempty"`
	Source         string           `json:"source,omitempty"`
	ExamYear       *int             `json:"exam_year,omitempty"`
	QuestionNo     string           `json:"question_no,omitempty"`
	IsRecollection bool             `json:"is_recollection,omitempty"`
	RubricPoints   []ai.RubricPoint `json:"rubric_points,omitempty"`
	RubricOrigin   string           `json:"rubric_origin,omitempty"`
	KPPath         []string         `json:"kp_path,omitempty"`
	KPID           *int64           `json:"kp_id,omitempty"`
	SourcePage     int              `json:"source_page,omitempty"`
}

// KPDraft 是待确认知识点。
type KPDraft struct {
	Name         string           `json:"name"`
	OriginalText string           `json:"original_text,omitempty"`
	KPPath       []string         `json:"kp_path"`
	RubricPoints []ai.RubricPoint `json:"rubric_points,omitempty"`
	SourcePage   int              `json:"source_page,omitempty"`
}

// 复核原因（契约 ReviewReason）。
const (
	ReasonLowConfidence     = "low_confidence"
	ReasonMissingAnswer     = "missing_answer"
	ReasonRubricUnconfirmed = "rubric_unconfirmed"
	ReasonRubricSumMismatch = "rubric_sum_mismatch"
	ReasonDuplicate         = "duplicate"
)

var reLeadNo = regexp.MustCompile(`^\s*(?:[（(]\s*)?\d{1,3}\s*[.．、)）]\s*`)

// LowConfidence 低于它的条目标「需核对」（PRD 1.7）。
const LowConfidence = 0.7

// needsReview：低置信度、采分点合计不等于分值、疑似重复要用户核对；缺答案、采分点待确认只是状态。
func needsReview(reasons []string) bool {
	for _, r := range reasons {
		if r == ReasonLowConfidence || r == ReasonRubricSumMismatch || r == ReasonDuplicate {
			return true
		}
	}
	return false
}

// reviewReasons 按题目内容重新判断与内容有关的原因，保留 low_confidence 与 duplicate（来自识别与去重）。
func reviewReasons(q QuestionDraft, keep []string) []string {
	var out []string
	for _, r := range keep {
		if r == ReasonLowConfidence || r == ReasonDuplicate {
			out = append(out, r)
		}
	}
	if strings.TrimSpace(q.Answer) == "" {
		out = append(out, ReasonMissingAnswer)
	}
	if ai.Subjective(q.QType) {
		if len(q.RubricPoints) > 0 && q.RubricOrigin != "user_confirmed" {
			out = append(out, ReasonRubricUnconfirmed)
		}
		if q.Score != nil && len(q.RubricPoints) > 0 && !rubricSumOK(q.RubricPoints, *q.Score) {
			out = append(out, ReasonRubricSumMismatch)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func rubricSumOK(points []ai.RubricPoint, score float64) bool {
	sum := 0.0
	for _, p := range points {
		sum += p.Score
	}
	d := sum - score
	return d > -0.01 && d < 0.01
}

func reasonsJSON(r []string) dbtypes.NullJSON {
	if len(r) == 0 {
		return nil
	}
	b, _ := json.Marshal(r)
	return b
}

func parseReasons(j dbtypes.NullJSON) []string {
	var r []string
	_ = json.Unmarshal(j, &r)
	return r
}

// Normalize 把题干归一化：去掉开头题号、空白与标点、统一小写，用于内容哈希去重。
func Normalize(stem string) string {
	stem = reLeadNo.ReplaceAllString(strings.TrimSpace(stem), "")
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, stem)
}

// ContentHash 是题干归一化后的哈希（questions.content_hash）。
func ContentHash(stem string) string {
	sum := sha256.Sum256([]byte(Normalize(stem)))
	return hex.EncodeToString(sum[:])
}

func hashOf(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

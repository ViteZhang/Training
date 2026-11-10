package rules

import "strings"

// AnswerTier 是参考答案的可信度档位（PRD v3 11.16，10 月 26 日更新），决定能否批改和是否计入预估分。
type AnswerTier string

const (
	// AnswerMaterial 来自你的资料：题目原文自带、从用户其他资料匹配到、或通过 1.7c 答案文件配对。批改正常，计入预估分。
	AnswerMaterial AnswerTier = "material"
	// AnswerConfirmed 你已确认：用户手动填写，或确认过 AI 参考答案。批改正常，计入预估分。
	AnswerConfirmed AnswerTier = "confirmed"
	// AnswerAI AI 参考答案：前两种都没有时由 AI 生成（用户端做到时生成，或官方题库由 AI 生成、经编辑审核）。
	// 照常批改并计入预估分（11 月 4 日产品更新，T49；此前标「仅供参考」不计入，已有的作答不追溯）；用户仍可确认或修改。
	AnswerAI AnswerTier = "ai"
	// AnswerAIQuestion 是 AI 出的变式题自带的答案：只基于用户确认过的知识点与采分点生成，与题库原题同样计分（10/26 与产品确认）。
	AnswerAIQuestion AnswerTier = "ai_question"
	// AnswerOfficial 官方答案：官方题库导入时标为团队答案的（T56）。此前归在「来自你的资料」，用户没导入过资料也看到这个说法。
	AnswerOfficial AnswerTier = "official"
	// AnswerMissing 还没有任何答案。
	AnswerMissing AnswerTier = "missing"
)

// TierOf 按题目的答案、答案来源（questions.answer_origin）与题目来源（questions.source）判断档位。
// 早期导入、没记答案来源的答案按「来自你的资料」处理。
func TierOf(answer, origin, source string) AnswerTier {
	if strings.TrimSpace(answer) == "" {
		return AnswerMissing
	}
	if source == "ai_generated" {
		return AnswerAIQuestion
	}
	switch origin {
	case "user_confirmed":
		return AnswerConfirmed
	case "ai_generated":
		return AnswerAI
	case "official":
		return AnswerOfficial
	}
	return AnswerMaterial
}

// CountsForEstimate 表示按这一档答案判分的作答是否计入预估分（PRD v3 11.6；11 月 4 日起 AI 参考答案也计入，T49）。
func (t AnswerTier) CountsForEstimate() bool {
	return t != AnswerMissing && t != ""
}

// PaperCountsForEstimate 判断一套真题卷能否参与估分：一半以上的主观题分值没有可信答案时暂不参与（PRD v3 11.6）。
// untrustedSubjective 是没有答案、或 11 月 4 日以前按 AI 参考答案批改（标了不计入）的主观题分值，subjective 是全部主观题分值。
func PaperCountsForEstimate(untrustedSubjective, subjective float64) bool {
	if subjective <= 0 {
		return true
	}
	return untrustedSubjective*2 <= subjective
}

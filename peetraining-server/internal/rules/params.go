package rules

import (
	"encoding/json"
	"fmt"
)

// Params 是 rule_params 表的强类型视图，字段名与 JSON 键一一对应。
// 默认值见 DefaultParams，与迁移 00008_seed_config.sql 一致（测试保证）。
type Params struct {
	Mastery       MasteryParams       `json:"mastery"`
	MasteryState  MasteryStateParams  `json:"mastery_state"`
	Review        ReviewParams        `json:"review_interval"`
	Stage         StageParams         `json:"stage"`
	PlanMix       map[Stage]PlanMix   `json:"plan_mix"`
	Plan          PlanParams          `json:"plan"`
	ScoreEstimate ScoreEstimateParams `json:"score_estimate"`
	LossDiagnosis LossDiagnosisParams `json:"loss_diagnosis"`
	WrongBook     WrongBookParams     `json:"wrong_book"`
	PaperTime     PaperTimeParams     `json:"paper_time"`
	AIPaper       AIPaperParams       `json:"ai_paper"`
	ExamProfile   ExamProfileParams   `json:"exam_profile"`
}

// MasteryParams 对应 PRD 11.1。
type MasteryParams struct {
	SelfAssess        map[SelfAssess]float64   `json:"self_assess"`
	ObjectiveCorrect  map[Difficulty]float64   `json:"objective_correct"`
	ObjectiveWrong    float64                  `json:"objective_wrong"`
	SubjectiveSlope   float64                  `json:"subjective_slope"`
	SubjectiveOffset  float64                  `json:"subjective_offset"`
	RevealAnswer      float64                  `json:"reveal_answer"`
	Recite            map[ReciteResult]float64 `json:"recite"`
	OverdueDaily      float64                  `json:"overdue_daily"`
	SecondaryKPFactor float64                  `json:"secondary_kp_factor"`
}

// MasteryStateParams 对应 PRD 11.2。
type MasteryStateParams struct {
	MasteredMinM            float64 `json:"mastered_min_m"`
	MasteredCorrectDays     int     `json:"mastered_correct_days"`
	MasteredWindowDays      int     `json:"mastered_window_days"`
	SubjectiveCorrectRate   float64 `json:"subjective_correct_rate"`
	WeakSectionAvg          float64 `json:"weak_section_avg"`
	FalseMasteryWindowDays  int     `json:"false_mastery_window_days"`
	FalseMasteryMinAttempts int     `json:"false_mastery_min_attempts"`
	FalseMasteryMaxRate     float64 `json:"false_mastery_max_rate"`
}

// ReviewParams 对应 PRD 11.3。
type ReviewParams struct {
	ResetDays          int     `json:"reset_days"`
	VagueDays          int     `json:"vague_days"`
	Steps              []int   `json:"steps"`
	SubjectiveLowRate  float64 `json:"subjective_low_rate"`
	SubjectiveHighRate float64 `json:"subjective_high_rate"`
}

// StageParams 对应 PRD 11.4。
type StageParams struct {
	FoundationMinDays   int     `json:"foundation_min_days"`
	StrengthenMinDays   int     `json:"strengthen_min_days"`
	SprintMinDays       int     `json:"sprint_min_days"`
	EarlySprintMaxDays  int     `json:"early_sprint_max_days"`
	EarlySprintCoverage float64 `json:"early_sprint_coverage"`
	SprintLowCoverage   float64 `json:"sprint_low_coverage"`
	SprintNewKPShare    float64 `json:"sprint_new_kp_share"`
}

// PlanMix 是某阶段今日计划的四组时间占比（PRD 11.5）。
type PlanMix struct {
	New    float64 `json:"new"`
	Review float64 `json:"review"`
	Weak   float64 `json:"weak"`
	Recite float64 `json:"recite"`
}

// PlanParams 对应 PRD 11.5 的单题用时与提分收益参数。
type PlanParams struct {
	Minutes     map[string]float64 `json:"minutes"`
	KPExamBonus float64            `json:"kp_exam_bonus"`
}

// ScoreEstimateParams 对应 PRD 11.6。
type ScoreEstimateParams struct {
	MeasuredWeight       float64            `json:"measured_weight"`
	ModelWeight          float64            `json:"model_weight"`
	MeasuredRecentPapers int                `json:"measured_recent_papers"`
	QTypeRecentQuestions int                `json:"qtype_recent_questions"`
	QTypeMinQuestions    int                `json:"qtype_min_questions"`
	Width                map[string]float64 `json:"width"`
}

// LossDiagnosisParams 对应 PRD 11.7。
type LossDiagnosisParams struct {
	KnowledgeMaxM  float64 `json:"knowledge_max_m"`
	TimeMinRatio   float64 `json:"time_min_ratio"`
	TailStartRatio float64 `json:"tail_start_ratio"`
}

// WrongBookParams 对应 PRD 11.8。
type WrongBookParams struct {
	RemoveAfterCorrectDays int     `json:"remove_after_correct_days"`
	SubjectiveCorrectRate  float64 `json:"subjective_correct_rate"`
}

// PaperTimeParams 对应 PRD 11.9。
type PaperTimeParams struct {
	DefaultTotalMinutes int                `json:"default_total_minutes"`
	CheckMinutes        int                `json:"check_minutes"`
	PerQuestionMinutes  map[string]float64 `json:"per_question_minutes"`
	RoundToMinutes      int                `json:"round_to_minutes"`
	OvertimeRatio       float64            `json:"overtime_ratio"`
	RemindLeftMinutes   int                `json:"remind_left_minutes"`
	ResumeWindowMinutes int                `json:"resume_window_minutes"`
}

// AIPaperParams 对应 PRD 11.10。
type AIPaperParams struct {
	ExamKPShareMin    float64 `json:"exam_kp_share_min"`
	ExamKPShareMax    float64 `json:"exam_kp_share_max"`
	TargetedWeakShare float64 `json:"targeted_weak_share"`
	WeakMaxM          float64 `json:"weak_max_m"`
	TargetedAvoidDays int     `json:"targeted_avoid_days"`
}

// ExamProfileParams 对应 PRD 11.11。
type ExamProfileParams struct {
	MinPapers            int     `json:"min_papers"`
	StructureRecentYears int     `json:"structure_recent_years"`
	HighFreqMinCount     int     `json:"high_freq_min_count"`
	MissingMinShare      float64 `json:"missing_min_share"`
	MissingKPRatio       float64 `json:"missing_kp_ratio"`
	MissingMaxMastery    float64 `json:"missing_max_mastery"`
}

// DefaultParams 返回 PRD 第 11 节的初始参数。
func DefaultParams() Params {
	return Params{
		Mastery: MasteryParams{
			SelfAssess:        map[SelfAssess]float64{SelfUnknown: 0, SelfVague: 30, SelfMastered: 50},
			ObjectiveCorrect:  map[Difficulty]float64{Easy: 10, Medium: 15, Hard: 20},
			ObjectiveWrong:    -15,
			SubjectiveSlope:   25,
			SubjectiveOffset:  -10,
			RevealAnswer:      -10,
			Recite:            map[ReciteResult]float64{ReciteRemembered: 8, ReciteVague: 2, ReciteForgot: -8},
			OverdueDaily:      -3,
			SecondaryKPFactor: 0.5,
		},
		MasteryState: MasteryStateParams{
			MasteredMinM: 80, MasteredCorrectDays: 2, MasteredWindowDays: 30, SubjectiveCorrectRate: 0.8,
			WeakSectionAvg: 40, FalseMasteryWindowDays: 7, FalseMasteryMinAttempts: 2, FalseMasteryMaxRate: 0.5,
		},
		Review: ReviewParams{ResetDays: 1, VagueDays: 2, Steps: []int{3, 7, 15, 30}, SubjectiveLowRate: 0.5, SubjectiveHighRate: 0.8},
		Stage: StageParams{
			FoundationMinDays: 150, StrengthenMinDays: 60, SprintMinDays: 14,
			EarlySprintMaxDays: 90, EarlySprintCoverage: 0.7, SprintLowCoverage: 0.6, SprintNewKPShare: 0.1,
		},
		PlanMix: map[Stage]PlanMix{
			Foundation: {New: 0.30, Review: 0.30, Weak: 0.20, Recite: 0.20},
			Strengthen: {New: 0.10, Review: 0.25, Weak: 0.45, Recite: 0.20},
			Sprint:     {New: 0, Review: 0.30, Weak: 0.50, Recite: 0.20},
			Final:      {New: 0, Review: 0.35, Weak: 0.20, Recite: 0.45},
		},
		Plan: PlanParams{
			Minutes:     map[string]float64{"objective": 1, "term": 3, "short_answer": 6, "discussion": 12, "recite": 1},
			KPExamBonus: 0.5,
		},
		ScoreEstimate: ScoreEstimateParams{
			MeasuredWeight: 0.6, ModelWeight: 0.4, MeasuredRecentPapers: 2, QTypeRecentQuestions: 20, QTypeMinQuestions: 5,
			Width: map[string]float64{"1": 0.10, "2": 0.06, "3": 0.04},
		},
		LossDiagnosis: LossDiagnosisParams{KnowledgeMaxM: 60, TimeMinRatio: 0.3, TailStartRatio: 0.67},
		WrongBook:     WrongBookParams{RemoveAfterCorrectDays: 2, SubjectiveCorrectRate: 0.8},
		PaperTime: PaperTimeParams{
			DefaultTotalMinutes: 180, CheckMinutes: 15,
			PerQuestionMinutes: map[string]float64{"term": 4, "short_answer": 15, "discussion": 40, "objective": 2, "essay": 60},
			RoundToMinutes:     5, OvertimeRatio: 0.2, RemindLeftMinutes: 15, ResumeWindowMinutes: 10,
		},
		AIPaper:     AIPaperParams{ExamKPShareMin: 0.5, ExamKPShareMax: 0.7, TargetedWeakShare: 0.5, WeakMaxM: 60, TargetedAvoidDays: 30},
		ExamProfile: ExamProfileParams{MinPapers: 2, StructureRecentYears: 3, HighFreqMinCount: 2, MissingMinShare: 0.1, MissingKPRatio: 0.5, MissingMaxMastery: 20},
	}
}

// ParseParams 用 rule_params 的行（param_key → JSON value）覆盖默认参数。
// 只覆盖出现的键；未知键忽略（其他模块的参数，如额度、价格，也在 rule_params 里）。
func ParseParams(rows map[string]json.RawMessage) (Params, error) {
	p := DefaultParams()
	targets := map[string]any{
		"mastery": &p.Mastery, "mastery_state": &p.MasteryState, "review_interval": &p.Review,
		"stage": &p.Stage, "plan_mix": &p.PlanMix, "plan": &p.Plan, "score_estimate": &p.ScoreEstimate,
		"loss_diagnosis": &p.LossDiagnosis, "wrong_book": &p.WrongBook, "paper_time": &p.PaperTime,
		"ai_paper": &p.AIPaper, "exam_profile": &p.ExamProfile,
	}
	for key, raw := range rows {
		target, ok := targets[key]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, target); err != nil {
			return Params{}, fmt.Errorf("rule_params.%s 格式错误：%w", key, err)
		}
	}
	return p, nil
}

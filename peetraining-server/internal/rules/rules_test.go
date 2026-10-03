package rules

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"
)

var P = DefaultParams()

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func d(y int, m time.Month, day int) Day { return DayFromDate(y, m, day) }

// ---- Day ----

func TestDay(t *testing.T) {
	// UTC 2026-10-01 16:00 是北京时间 10 月 2 日 0 点。
	got := DayOf(time.Date(2026, 10, 1, 16, 0, 0, 0, time.UTC))
	if got.String() != "2026-10-02" {
		t.Errorf("DayOf = %s", got)
	}
	if got.Time() != time.Date(2026, 10, 1, 16, 0, 0, 0, time.UTC) {
		t.Errorf("Time = %v", got.Time())
	}
	if got.AddDays(3).String() != "2026-10-05" {
		t.Error("AddDays")
	}
}

// ---- 参数 ----

func TestParseParams(t *testing.T) {
	p, err := ParseParams(map[string]json.RawMessage{
		"mastery":     json.RawMessage(`{"objective_wrong": -20}`),
		"unknown_key": json.RawMessage(`{"x":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Mastery.ObjectiveWrong != -20 {
		t.Errorf("应覆盖出现的字段：%v", p.Mastery.ObjectiveWrong)
	}
	if p.Mastery.SubjectiveSlope != 25 {
		t.Error("未出现的字段保留默认值")
	}
	if _, err := ParseParams(map[string]json.RawMessage{"stage": json.RawMessage(`[`)}); err == nil {
		t.Error("坏 JSON 应报错")
	}
	all, _ := ParseParams(nil)
	if !reflect.DeepEqual(all, DefaultParams()) {
		t.Error("空输入应等于默认参数")
	}
}

func TestTypesHelpers(t *testing.T) {
	for _, q := range []QType{"single_choice", "multi_choice", "true_false", "fill_blank"} {
		if !IsObjective(q) || MinutesKey(q) != "objective" {
			t.Errorf("%s 应为客观题", q)
		}
	}
	if IsObjective(QTermExplain) || MinutesKey(QTermExplain) != "term" {
		t.Error("名词解释不是客观题")
	}
}

// ---- 11.1 掌握分 ----

func TestApplyMasteryPRDTable(t *testing.T) {
	mp := P.Mastery
	cases := []struct {
		name     string
		m        float64
		ev       MasteryEvent
		answered bool
		want     float64
	}{
		{"自评不会", 70, MasteryEvent{Kind: EventSelfAssess, SelfAssess: SelfUnknown}, false, 0},
		{"自评模糊", 0, MasteryEvent{Kind: EventSelfAssess, SelfAssess: SelfVague}, false, 30},
		{"自评掌握最高 50", 0, MasteryEvent{Kind: EventSelfAssess, SelfAssess: SelfMastered}, false, 50},
		{"已有作答记录后自评不改 M（D16）", 70, MasteryEvent{Kind: EventSelfAssess, SelfAssess: SelfVague}, true, 70},
		{"客观题答对 易", 50, MasteryEvent{Kind: EventObjectiveCorrect, Difficulty: Easy}, true, 60},
		{"客观题答对 中", 50, MasteryEvent{Kind: EventObjectiveCorrect, Difficulty: Medium}, true, 65},
		{"客观题答对 难", 50, MasteryEvent{Kind: EventObjectiveCorrect, Difficulty: Hard}, true, 70},
		{"难度未知按中", 50, MasteryEvent{Kind: EventObjectiveCorrect}, true, 65},
		{"客观题答错", 50, MasteryEvent{Kind: EventObjectiveWrong}, true, 35},
		{"主观题满分 +15", 50, MasteryEvent{Kind: EventSubjective, ScoreRate: 1}, true, 65},
		{"主观题零分 −10", 50, MasteryEvent{Kind: EventSubjective, ScoreRate: 0}, true, 40},
		{"主观题 60%", 50, MasteryEvent{Kind: EventSubjective, ScoreRate: 0.6}, true, 55},
		{"得分率超过 1 截断", 50, MasteryEvent{Kind: EventSubjective, ScoreRate: 1.5}, true, 65},
		{"看答案", 50, MasteryEvent{Kind: EventRevealAnswer}, true, 40},
		{"背诵记住了", 50, MasteryEvent{Kind: EventRecite, Recite: ReciteRemembered}, true, 58},
		{"背诵模糊", 50, MasteryEvent{Kind: EventRecite, Recite: ReciteVague}, true, 52},
		{"背诵没记住", 50, MasteryEvent{Kind: EventRecite, Recite: ReciteForgot}, true, 42},
		{"非主知识点减半", 50, MasteryEvent{Kind: EventObjectiveWrong, Secondary: true}, true, 42.5},
		{"上限 100", 95, MasteryEvent{Kind: EventObjectiveCorrect, Difficulty: Hard}, true, 100},
		{"下限 0", 5, MasteryEvent{Kind: EventObjectiveWrong}, true, 0},
	}
	for _, c := range cases {
		if got := ApplyMastery(c.m, c.ev, c.answered, mp); !near(got, c.want) {
			t.Errorf("%s：got %v want %v", c.name, got, c.want)
		}
	}
}

func TestApplyOverdue(t *testing.T) {
	mp := P.Mastery
	review := d(2026, 10, 10)
	// 复习日当天不衰减。
	if m, _ := ApplyOverdue(50, review, 0, review, mp); m != 50 {
		t.Errorf("当天不衰减：%v", m)
	}
	// 逾期 3 天 −9。
	m, to := ApplyOverdue(50, review, 0, d(2026, 10, 13), mp)
	if m != 41 || to != d(2026, 10, 13) {
		t.Errorf("逾期 3 天：%v %s", m, to)
	}
	// 再过 1 天只再扣 3，不重复扣。
	m2, _ := ApplyOverdue(m, review, to, d(2026, 10, 14), mp)
	if m2 != 38 {
		t.Errorf("增量衰减：%v", m2)
	}
	// 同一天重复调用不变。
	if m3, to3 := ApplyOverdue(m2, review, d(2026, 10, 14), d(2026, 10, 14), mp); m3 != 38 || to3 != d(2026, 10, 14) {
		t.Error("同一天重复调用应不变")
	}
	// 没有排期不衰减；衰减截断在 0。
	if m4, _ := ApplyOverdue(50, 0, 0, d(2026, 10, 14), mp); m4 != 50 {
		t.Error("没有排期不衰减")
	}
	if m5, _ := ApplyOverdue(5, review, 0, d(2026, 12, 1), mp); m5 != 0 {
		t.Error("衰减截断在 0")
	}
}

// ---- 11.2 掌握状态 ----

func TestStateOf(t *testing.T) {
	sp := P.MasteryState
	today := d(2026, 10, 30)
	cases := []struct {
		name string
		f    MasteryFacts
		want MasteryState
	}{
		{"什么都没有", MasteryFacts{}, StateUnlearned},
		{"只浏览", MasteryFacts{Viewed: true}, StateLearning},
		{"只自评", MasteryFacts{Assessed: true, M: 50}, StateLearning},
		{"作答过、M 不够", MasteryFacts{Answered: true, M: 79, CorrectDays: []Day{today, today - 1}}, StateConsolidating},
		{"M 够、只有 1 个日期", MasteryFacts{Answered: true, M: 85, CorrectDays: []Day{today, today}}, StateConsolidating},
		{"M 够、2 个日期在 30 天内", MasteryFacts{Answered: true, M: 80, CorrectDays: []Day{today - 29, today}}, StateMastered},
		{"其中一个超出 30 天", MasteryFacts{Answered: true, M: 90, CorrectDays: []Day{today - 30, today}}, StateConsolidating},
		{"未来日期不算", MasteryFacts{Answered: true, M: 90, CorrectDays: []Day{today + 1, today}}, StateConsolidating},
	}
	for _, c := range cases {
		if got := StateOf(c.f, today, sp); got != c.want {
			t.Errorf("%s：got %s want %s", c.name, got, c.want)
		}
	}
}

func TestTrimCorrectDays(t *testing.T) {
	today := d(2026, 10, 30)
	got := TrimCorrectDays([]Day{today, today - 40, today - 3, today, today + 2}, today, 30)
	want := []Day{today - 3, today}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestIsCorrect(t *testing.T) {
	if !IsCorrect(true, true, 0, 0.8) || IsCorrect(true, false, 1, 0.8) {
		t.Error("客观题看对错")
	}
	if !IsCorrect(false, false, 0.8, 0.8) || IsCorrect(false, true, 0.79, 0.8) {
		t.Error("主观题得分率 ≥ 80% 视为答对")
	}
}

func TestFalseMastery(t *testing.T) {
	sp := P.MasteryState
	if !FalseMastery(SelfMastered, []bool{true, false, false}, sp) {
		t.Error("自评掌握、3 次对 1 次 → 以为会了")
	}
	if FalseMastery(SelfMastered, []bool{true, false}, sp) {
		t.Error("正确率 50% 不算（要求 < 50%）")
	}
	if FalseMastery(SelfMastered, []bool{false}, sp) {
		t.Error("作答不足 2 次不算")
	}
	if FalseMastery(SelfVague, []bool{false, false}, sp) {
		t.Error("最近自评不是掌握不算")
	}
}

func TestSectionMastery(t *testing.T) {
	sp := P.MasteryState
	if avg, weak := SectionMastery([]float64{20, 40, 50}, sp); !near(avg, 110.0/3) || !weak {
		t.Errorf("avg=%v weak=%v", avg, weak)
	}
	if _, weak := SectionMastery([]float64{40}, sp); weak {
		t.Error("等于 40 不算短板")
	}
	if avg, weak := SectionMastery(nil, sp); avg != 0 || weak {
		t.Error("空板块")
	}
}

// ---- 11.3 复习间隔 ----

func TestNextReview(t *testing.T) {
	rp := P.Review
	today := d(2026, 10, 1)
	steps := []struct {
		outcome  ReviewOutcome
		wantDays int
		wantStep int
	}{
		{ReviewPass, 3, 1},  // 首次 3 天后
		{ReviewPass, 7, 2},  // 之后 7
		{ReviewVague, 2, 2}, // 模糊 2 天后，序列不前进
		{ReviewPass, 15, 3},
		{ReviewPass, 30, 4},
		{ReviewPass, 30, 5}, // 超过末档保持 30
		{ReviewFail, 1, 0},  // 答错 1 天后，序列重置
		{ReviewPass, 3, 1},
	}
	step := 0
	for i, s := range steps {
		next, ns := NextReview(s.outcome, step, today, rp)
		if int(next-today) != s.wantDays || ns != s.wantStep {
			t.Errorf("第 %d 步：+%d 天 step %d，want +%d step %d", i, next-today, ns, s.wantDays, s.wantStep)
		}
		step = ns
	}
}

func TestOutcomeMapping(t *testing.T) {
	rp := P.Review
	for rate, want := range map[float64]ReviewOutcome{0.49: ReviewFail, 0.5: ReviewVague, 0.79: ReviewVague, 0.8: ReviewPass, 1: ReviewPass} {
		if got := SubjectiveOutcome(rate, rp); got != want {
			t.Errorf("rate %v → %v want %v", rate, got, want)
		}
	}
	if ReciteOutcome(ReciteRemembered) != ReviewPass || ReciteOutcome(ReciteVague) != ReviewVague || ReciteOutcome(ReciteForgot) != ReviewFail {
		t.Error("背诵映射")
	}
}

// ---- 11.4 备考阶段 ----

func TestDefaultStage(t *testing.T) {
	sp := P.Stage
	for days, want := range map[int]Stage{200: Foundation, 150: Foundation, 149: Strengthen, 60: Strengthen, 59: Sprint, 14: Sprint, 13: Final, 0: Final} {
		if got := DefaultStage(days, sp); got != want {
			t.Errorf("%d 天：got %s want %s", days, got, want)
		}
	}
}

func TestCoverage(t *testing.T) {
	// 权重：1+0=1（已学）、1+2=3（未学）、1+1=2（已学）→ 3/6
	got := Coverage([]CoverageKP{
		{State: StateLearning}, {State: StateUnlearned, ExamCount: 2}, {State: StateMastered, ExamCount: 1},
	})
	if !near(got, 0.5) {
		t.Errorf("coverage = %v", got)
	}
	if Coverage(nil) != 0 {
		t.Error("空")
	}
}

func TestAdviseStage(t *testing.T) {
	sp := P.Stage
	// 到点：强化期用户距考试 59 天 → 提示冲刺期。
	a := AdviseStage(Strengthen, 59, 0.8, sp)
	if !a.Prompt || a.Suggested != Sprint || a.Reason != "by_date" || a.LowCoverage {
		t.Errorf("到点：%+v", a)
	}
	// 准备度修正：强化期、≤ 90 天、覆盖率 ≥ 70% → 提示可提前冲刺。
	a = AdviseStage(Strengthen, 90, 0.7, sp)
	if !a.Prompt || a.Suggested != Sprint || a.Reason != "early_sprint" {
		t.Errorf("提前冲刺：%+v", a)
	}
	// 覆盖率不够不提前。
	if a = AdviseStage(Strengthen, 80, 0.69, sp); a.Prompt {
		t.Errorf("覆盖率不够：%+v", a)
	}
	// 91 天不提前。
	if a = AdviseStage(Strengthen, 91, 0.9, sp); a.Prompt {
		t.Errorf("91 天：%+v", a)
	}
	// 冲刺期覆盖率 < 60%：提示先补新知识点，保留 10%。
	a = AdviseStage(Sprint, 40, 0.5, sp)
	if a.Prompt || !a.LowCoverage || a.NewKPShare != 0.1 {
		t.Errorf("冲刺期覆盖率低：%+v", a)
	}
	// 用户手动选了更靠后的阶段：日期没追上，不提示、不覆盖。
	if a = AdviseStage(Final, 100, 0.2, sp); a.Prompt || a.Suggested != Final {
		t.Errorf("手动选择后不覆盖：%+v", a)
	}
}

// ---- 11.5 今日计划 ----

func TestMixFor(t *testing.T) {
	pm := P.PlanMix
	if got := MixFor(Sprint, false, 0.1, pm); got != pm[Sprint] {
		t.Error("覆盖率正常时用原占比")
	}
	got := MixFor(Sprint, true, 0.1, pm)
	if !near(got.New, 0.1) || !near(got.New+got.Review+got.Weak+got.Recite, 1) || !near(got.Review/got.Weak, 0.3/0.5) {
		t.Errorf("冲刺期保留 10%% 新知识点：%+v", got)
	}
	if got := MixFor(Foundation, true, 0.1, pm); got != pm[Foundation] {
		t.Error("新知识点占比已经够时不变")
	}
}

func TestKPWeightAndCoef(t *testing.T) {
	pp := P.Plan
	if got := KPWeight(0.3, 2, true, 5, pp); !near(got, 0.6) {
		t.Errorf("0.3 × (1 + 0.5 × 2) = %v", got)
	}
	if got := KPWeight(0.3, 2, false, 4, pp); !near(got, 0.25) {
		t.Errorf("没有真题时板块等权：%v", got)
	}
	if KPWeight(0, 0, false, 0, pp) != 1 {
		t.Error("没有板块")
	}
	each := map[QType]float64{QTermExplain: 5, QShortAnswer: 10, QDiscussion: 25}
	if !near(QTypeCoef(QShortAnswer, each), 0.4) || !near(QTypeCoef(QDiscussion, each), 1) {
		t.Error("按最大值归一")
	}
	if !near(QTypeCoef("single_choice", each), 0.2) {
		t.Error("真题里没有的题型取最小系数")
	}
	if QTypeCoef(QTermExplain, nil) != 1 || QTypeCoef(QTermExplain, map[QType]float64{QTermExplain: 0}) != 1 {
		t.Error("没有真题卷时为 1")
	}
	if !near(Gain(0.6, 40, 0.5), 0.18) || Gain(1, 120, 1) != 0 {
		t.Error("提分收益")
	}
}

func TestItemMinutes(t *testing.T) {
	pp := P.Plan
	cases := map[QType]float64{"single_choice": 1, QTermExplain: 3, QShortAnswer: 6, QDiscussion: 12, "calculation": 12}
	for q, want := range cases {
		if got := ItemMinutes(q, false, pp); got != want {
			t.Errorf("%s: %v", q, got)
		}
	}
	if ItemMinutes(QDiscussion, true, pp) != 1 {
		t.Error("背诵 1 分钟 / 条")
	}
}

func TestBuildPlan(t *testing.T) {
	pp := P.Plan
	mix := P.PlanMix[Strengthen] // 新 10%、复习 25%、薄弱 45%、背诵 20%
	in := PlanInput{
		BudgetMinutes: 60, // 两门课，每门 30 分钟
		Mix:           mix,
		Subjects: []SubjectCandidates{
			{SubjectID: 1, Candidates: []PlanCandidate{
				{Group: GroupNew, QuestionID: 1, KPID: 10, QType: QTermExplain, Gain: 0.9}, // 新 3 分钟预算：放得下 3
				{Group: GroupNew, QuestionID: 2, KPID: 11, QType: QTermExplain, Gain: 0.5}, // 放不下
				{Group: GroupReview, QuestionID: 3, KPID: 12, QType: QShortAnswer, Gain: 0.2},
				{Group: GroupReview, QuestionID: 4, KPID: 13, QType: "single_choice", Gain: 0.1},
				{Group: GroupWeak, QuestionID: 5, KPID: 12, QType: QShortAnswer, Gain: 0.9}, // 同一知识点当日只出现一次
				{Group: GroupWeak, QuestionID: 6, KPID: 14, QType: QDiscussion, Gain: 0.8},  // 12 分钟
				{Group: GroupRecite, KPID: 12, Gain: 0.5},                                   // 背诵与做题分开算
				{Group: GroupRecite, KPID: 12, Gain: 0.4},                                   // 同一条背诵不重复
			}},
			{SubjectID: 2, Candidates: []PlanCandidate{{Group: GroupRecite, KPID: 20}}}, // 没有题目：份额给其他课
			{SubjectID: 3, Candidates: []PlanCandidate{
				{Group: GroupWeak, QuestionID: 30, KPID: 30, QType: "single_choice", Gain: 0.3},
			}},
		},
	}
	plan := BuildPlan(in, pp)
	var ids []int64
	for _, it := range plan.Items {
		ids = append(ids, it.QuestionID)
	}
	// 客观题在前（4、30），主观题在后（1、3、6），背诵最后（0）。
	want := []int64{4, 30, 1, 3, 6, 0}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("计划顺序 %v want %v", ids, want)
	}
	if !near(plan.TotalMinutes, 1+1+3+6+12+1) {
		t.Errorf("总用时 %v", plan.TotalMinutes)
	}
	if plan.Shortfall[3][GroupWeak] < 1 || plan.Shortfall[1][GroupWeak] < 1 {
		t.Errorf("没填满的组要报缺口：%v", plan.Shortfall)
	}
	if _, ok := plan.Shortfall[2]; ok {
		t.Error("没有题目的课不参与分配")
	}
	if empty := BuildPlan(PlanInput{BudgetMinutes: 45, Mix: mix}, pp); len(empty.Items) != 0 {
		t.Error("没有任何题目时计划为空")
	}
}

// ---- 11.6 预估分 ----

func TestEstimateScore(t *testing.T) {
	ep := P.ScoreEstimate
	if _, ok := EstimateScore(EstimateInput{FullScore: 150}, ep); ok {
		t.Error("没做过真题卷不显示预估分")
	}
	rates := func(v float64, n int) []float64 {
		r := make([]float64, n)
		for i := range r {
			r[i] = v
		}
		return r
	}
	in := EstimateInput{
		FullScore: 150,
		Papers:    []PaperScore{{Score: 100}, {Score: 90}, {Score: 60}}, // 实测取最近 2 套：95
		QTypes: []QTypeStat{
			{QType: QTermExplain, TotalInPaper: 30, RecentRates: rates(0.8, 25)},                    // 近 20 题 0.8 → 24
			{QType: QShortAnswer, TotalInPaper: 40, RecentRates: rates(0.5, 3), LastPaperRate: 0.6}, // 不足 5 题用整卷 0.6 → 24
			{QType: QDiscussion, TotalInPaper: 80, RecentRates: rates(0.5, 10)},                     // 0.5 → 40，失分 40 最大
		},
		RecentSubjectiveCount: 20,
	}
	e, ok := EstimateScore(in, ep)
	if !ok {
		t.Fatal("应有预估分")
	}
	// 中值 = 0.6 × 95 + 0.4 × 88 = 92.2；3 套卷 ±4% → 88.5 / 95.9 → 89–96
	if !near(e.Measured, 95) || !near(e.Model, 88) || !near(e.Mid, 92.2) {
		t.Errorf("measured %v model %v mid %v", e.Measured, e.Model, e.Mid)
	}
	if e.Low != 89 || e.High != 96 || e.BasisPapers != 3 || e.BasisQuestions != 20 || e.MainGap != QDiscussion {
		t.Errorf("%+v", e)
	}
	// 只做过 1 套：实测取该套，±10%。
	one, _ := EstimateScore(EstimateInput{FullScore: 150, Papers: []PaperScore{{Score: 100}}}, ep)
	if !near(one.Measured, 100) || one.Low != 54 || one.High != 66 {
		t.Errorf("1 套卷：%+v", one)
	}
	// 区间截断在满分。
	top, _ := EstimateScore(EstimateInput{FullScore: 100, Papers: []PaperScore{{Score: 100}, {Score: 100}},
		QTypes: []QTypeStat{{QType: QDiscussion, TotalInPaper: 100, LastPaperRate: 1}}}, ep)
	if top.High != 100 {
		t.Errorf("截断：%+v", top)
	}
}

func TestScalePaperScore(t *testing.T) {
	// 缺题卷：实际收录 120 分的题得了 90，换算到 150 满分 = 112.5
	if !near(ScalePaperScore(90, 120, 150), 112.5) || ScalePaperScore(1, 0, 150) != 0 {
		t.Error("换算")
	}
}

func TestWidthFor(t *testing.T) {
	w := map[string]float64{"1": 0.1, "2": 0.06, "3": 0.04, "x": 9}
	if widthFor(1, w) != 0.1 || widthFor(2, w) != 0.06 || widthFor(7, w) != 0.04 || widthFor(0, w) != 0 {
		t.Error("区间宽度档位")
	}
}

// ---- 11.7 失分诊断 ----

func TestClassifyLoss(t *testing.T) {
	lp := P.LossDiagnosis
	cases := []struct {
		name string
		in   LossInput
		want LossType
		ok   bool
	}{
		{"没有失分", LossInput{LostScore: 0}, "", false},
		{"知识没掌握", LossInput{LostScore: 3, MissedOrPartial: true, KPM: 59}, LossKnowledge, true},
		{"答题不规范：知道但没写到位", LossInput{LostScore: 3, MissedOrPartial: true, KPM: 60}, LossNorm, true},
		{"答题不规范：结构缺层次", LossInput{LostScore: 2, StructureLacking: true, KPM: 30}, LossNorm, true},
		{"限时：未作答", LossInput{LostScore: 10, Timed: true, Unanswered: true}, LossTime, true},
		{"限时：后段且用时不足 30%", LossInput{LostScore: 10, Timed: true, MissedOrPartial: true, KPM: 30, Position: 0.9, TimeSpentSeconds: 100, SuggestedSeconds: 600}, LossTime, true},
		{"限时：后段但用时够", LossInput{LostScore: 10, Timed: true, MissedOrPartial: true, KPM: 30, Position: 0.9, TimeSpentSeconds: 300, SuggestedSeconds: 600}, LossKnowledge, true},
		{"限时：前段用时短不算时间", LossInput{LostScore: 10, Timed: true, MissedOrPartial: true, KPM: 70, Position: 0.2, TimeSpentSeconds: 10, SuggestedSeconds: 600}, LossNorm, true},
		{"不限时未作答算知识", LossInput{LostScore: 5, Unanswered: true, KPM: 90}, LossKnowledge, true},
	}
	for _, c := range cases {
		got, ok := ClassifyLoss(c.in, lp)
		if got != c.want || ok != c.ok {
			t.Errorf("%s：got %s %v", c.name, got, ok)
		}
	}
	agg := AggregateLoss([]LossInput{cases[1].in, cases[2].in, cases[4].in, cases[0].in}, lp)
	if agg[LossKnowledge] != 3 || agg[LossNorm] != 3 || agg[LossTime] != 10 {
		t.Errorf("汇总：%v", agg)
	}
}

// ---- 11.8 错题本 ----

func TestWrongBook(t *testing.T) {
	for _, c := range []struct {
		obj, correct bool
		rate         float64
		revealed     bool
		reason       WrongReason
		add          bool
	}{
		{true, false, 0, false, WrongWrong, true},
		{true, true, 1, false, "", false},
		{false, false, 0.99, false, WrongPartial, true},
		{false, true, 1, false, "", false},
		{true, true, 1, true, WrongRevealed, true},
	} {
		r, add := WrongBookAdd(c.obj, c.correct, c.rate, c.revealed)
		if r != c.reason || add != c.add {
			t.Errorf("%+v → %s %v", c, r, add)
		}
	}
	wp := P.WrongBook
	day1, day2 := d(2026, 10, 1), d(2026, 10, 3)
	days, out := WrongBookProgress(nil, true, day1, wp)
	if out || len(days) != 1 {
		t.Fatal("第一次答对不移出")
	}
	days, out = WrongBookProgress(days, true, day1, wp)
	if out || len(days) != 1 {
		t.Fatal("同一天再答对不算第 2 个日期")
	}
	if d2, out2 := WrongBookProgress(days, false, day2, wp); out2 || len(d2) != 0 {
		t.Fatal("答错清零")
	}
	if _, out = WrongBookProgress(days, true, day2, wp); !out {
		t.Fatal("2 个不同日期连续答对自动移出")
	}
}

// ---- 11.9 整卷时间 ----

func TestSuggestedTimes(t *testing.T) {
	pt := P.PaperTime
	// 654 示例结构：名词解释 6、简答 4、论述 2 → 原始 24 + 60 + 80 = 164，缩放到 165
	got := SuggestedTimes([]SectionCount{{QTermExplain, 6}, {QShortAnswer, 4}, {QDiscussion, 2}}, 180, pt)
	want := []SectionTime{{QTermExplain, 25, 25}, {QShortAnswer, 60, 85}, {QDiscussion, 80, 165}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
	// 默认总时长；未知题型按论述；客观题 2 分钟。
	got = SuggestedTimes([]SectionCount{{"single_choice", 10}, {"other", 1}}, 0, pt)
	if got[0].Minutes != 55 || got[1].Minutes != 110 {
		t.Errorf("默认与未知题型：%+v", got)
	}
	if got := SuggestedTimes([]SectionCount{{QTermExplain, 0}}, 180, pt); got[0].Minutes != 0 {
		t.Error("没有题")
	}
}

func TestPaperTimeHelpers(t *testing.T) {
	pt := P.PaperTime
	if !Overtime(31, 25, pt) || Overtime(30, 25, pt) {
		t.Error("超过建议 20% 记为超时")
	}
	// 4.25 设计稿：名词解释建议 25′ 实际 38′ 超时；论述建议 80′ 实际 70′ 正常；检查建议 15′ 实际 0′ 少用。
	for _, c := range []struct {
		actual, suggested float64
		want              TimeStatus
	}{{38, 25, TimeOvertime}, {70, 80, TimeOK}, {64, 80, TimeOK}, {63, 80, TimeUnder}, {0, 15, TimeUnder}, {5, 0, TimeOK}} {
		if got := CompareTime(c.actual, c.suggested, pt); got != c.want {
			t.Errorf("CompareTime(%v, %v) = %s，应为 %s", c.actual, c.suggested, got, c.want)
		}
	}
	loss := TimeLoss([]UnansweredQuestion{{QDiscussion, 25}, {QShortAnswer, 10}, {QTermExplain, 5}},
		map[QType]float64{QDiscussion: 0.6, QShortAnswer: 1.2})
	if !near(loss, 25) {
		t.Errorf("时间失分 15 + 10 + 0 = %v", loss)
	}
	start := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	dl := MockDeadline(start, 180)
	if dl != start.Add(3*time.Hour) {
		t.Error("截止时间")
	}
	if !ShouldWarnTimeLeft(dl, dl.Add(-15*time.Minute), pt) || ShouldWarnTimeLeft(dl, dl.Add(-16*time.Minute), pt) || ShouldWarnTimeLeft(dl, dl, pt) {
		t.Error("剩 15 分钟提示")
	}
	at := start.Add(time.Hour)
	if !CanResume(at, at.Add(10*time.Minute), false, pt) {
		t.Error("10 分钟内可恢复")
	}
	if CanResume(at, at.Add(11*time.Minute), false, pt) || CanResume(at, at.Add(time.Minute), true, pt) ||
		CanResume(time.Time{}, at, false, pt) || CanResume(at, at.Add(-time.Minute), false, pt) {
		t.Error("超时、已用过、未中断都不能恢复")
	}
}

// ---- 11.10 AI 组卷 ----

func TestComposeStandardPaper(t *testing.T) {
	ap := P.AIPaper
	in := ComposeInput{
		Kind:  PaperStandard,
		Slots: []PaperSlot{{QTermExplain, 4, 5}, {QDiscussion, 1, 30}},
		Candidates: []PaperCandidate{
			{QuestionID: 1, KPID: 1, SectionID: 1, QType: QTermExplain, ExamKP: true, DaysSinceDone: -1},
			{QuestionID: 2, KPID: 1, SectionID: 1, QType: QTermExplain, ExamKP: true, DaysSinceDone: -1}, // 同一知识点不重复
			{QuestionID: 3, KPID: 2, SectionID: 2, QType: QTermExplain, ExamKP: true, DaysSinceDone: -1},
			{QuestionID: 4, KPID: 3, SectionID: 2, QType: QTermExplain, ExamKP: false, DaysSinceDone: -1},
			{QuestionID: 5, KPID: 4, SectionID: 1, QType: QTermExplain, ExamKP: true, DoneInPaper: true}, // 做过的真题不出现
			{QuestionID: 6, KPID: 5, SectionID: 1, QType: QTermExplain, ExamKP: false, DaysSinceDone: 3}, // 标准卷不避开近期
			{QuestionID: 7, KPID: 6, SectionID: 2, QType: QDiscussion, ExamKP: true, DaysSinceDone: -1},
		},
		SectionShares: map[int64]float64{1: 0.5, 2: 0.5},
	}
	res := ComposePaper(in, ap)
	got := map[int64]bool{}
	for _, it := range res.Items {
		got[it.QuestionID] = true
	}
	if len(res.Items) != 5 || got[2] || got[5] || !got[7] || len(res.Shortfall) != 0 {
		t.Errorf("标准卷：%+v shortfall %v", res.Items, res.Shortfall)
	}
	if res.Items[4].Seq != 5 || res.Items[4].Score != 30 {
		t.Errorf("题号与分值：%+v", res.Items[4])
	}
}

func TestComposeTargetedPaper(t *testing.T) {
	ap := P.AIPaper
	in := ComposeInput{
		Kind:  PaperTargeted,
		Slots: []PaperSlot{{QShortAnswer, 2, 10}, {QDiscussion, 1, 30}},
		Candidates: []PaperCandidate{
			{QuestionID: 1, KPID: 1, QType: QShortAnswer, M: 90, DaysSinceDone: -1},
			{QuestionID: 2, KPID: 2, QType: QShortAnswer, M: 30, DaysSinceDone: -1},
			{QuestionID: 3, KPID: 3, QType: QShortAnswer, M: 20, DaysSinceDone: 10}, // 近 30 天做过，避开
			{QuestionID: 4, KPID: 4, QType: QShortAnswer, M: 50, DaysSinceDone: 40},
		},
	}
	res := ComposePaper(in, ap)
	ids := []int64{}
	for _, it := range res.Items {
		ids = append(ids, it.QuestionID)
	}
	// 薄弱目标 round(3×0.5)=2：先选薄弱的 2（M 低优先）、再选 4（也是薄弱，仍可）；论述没有题 → 缺 1 道由变式题补足
	if !reflect.DeepEqual(ids, []int64{2, 4}) || res.Shortfall[QDiscussion] != 1 {
		t.Errorf("针对卷：%v shortfall %v", ids, res.Shortfall)
	}
}

// ---- 11.11 考情分析 ----

func TestBuildExamProfile(t *testing.T) {
	ep := P.ExamProfile
	mk := func(year int, q QType, n int, score float64, section int64, kps ...int64) []ExamQuestion {
		out := make([]ExamQuestion, n)
		for i := range out {
			out[i] = ExamQuestion{Year: year, QType: q, Score: score, SectionID: section, KPIDs: kps}
		}
		return out
	}
	var qs []ExamQuestion
	// 2021：旧结构；2022–2024：名词解释 6×5、简答 4×15、论述 2×30
	qs = append(qs, mk(2021, QTermExplain, 10, 3, 1)...)
	for _, y := range []int{2022, 2023, 2024} {
		qs = append(qs, mk(y, QTermExplain, 6, 5, 1, 100)...)
		qs = append(qs, mk(y, QShortAnswer, 4, 15, 2)...)
		qs = append(qs, mk(y, QDiscussion, 2, 30, 3, 200)...)
	}
	qs = append(qs, ExamQuestion{Year: 2024, QType: QTermExplain, Score: 5, Recollection: true}) // 回忆版
	qs = append(qs, ExamQuestion{Year: 2024, QType: QTermExplain})                               // 缺分值
	qs = append(qs, ExamQuestion{QType: QTermExplain, Score: 5})                                 // 缺年份

	p := BuildExamProfile(qs, ep)
	if !p.Ready || p.PaperCount != 4 || p.Excluded != 3 {
		t.Fatalf("%+v", p)
	}
	want := []StructureSlot{{QTermExplain, 6, 5, 30}, {QShortAnswer, 4, 15, 60}, {QDiscussion, 2, 30, 60}}
	if !reflect.DeepEqual(p.Structure, want) || p.StableYears != 3 || len(p.ChangedYears) != 0 {
		t.Errorf("结构：%+v stable %d changed %v", p.Structure, p.StableYears, p.ChangedYears)
	}
	// 板块 1：2021 的 30 + 3 年 × 30 = 120；总分 30 + 3 × 150 = 480
	if !near(p.SectionShares[1], 120.0/480) || !near(p.SectionShares[2], 180.0/480) {
		t.Errorf("板块占比：%v", p.SectionShares)
	}
	// 知识点 100 出现 18 次、200 出现 6 次。
	if len(p.HighFreq) != 2 || p.HighFreq[0] != (KPCount{100, 18}) || p.HighFreq[1] != (KPCount{200, 6}) {
		t.Errorf("高频考点：%v", p.HighFreq)
	}

	// 只有 1 套卷：提示再导入。
	one := BuildExamProfile(mk(2024, QTermExplain, 3, 5, 1), ep)
	if one.Ready || one.PaperCount != 1 {
		t.Errorf("1 套：%+v", one)
	}

	// 结构变化：最近一年变了，近 3 年里出现最多的仍是旧结构，标出变化年份。
	var changed []ExamQuestion
	changed = append(changed, mk(2022, QTermExplain, 2, 5, 1)...)
	changed = append(changed, mk(2023, QTermExplain, 2, 5, 1)...)
	changed = append(changed, mk(2024, QTermExplain, 3, 5, 1)...)
	changed = append(changed, mk(2024, "calculation", 1, 10, 1)...)
	c := BuildExamProfile(changed, ep)
	if c.StableYears != 0 || !reflect.DeepEqual(c.ChangedYears, []int{2024}) || c.Structure[0].Count != 2 {
		t.Errorf("结构变化：%+v", c)
	}
	if c2 := structureOf(mk(2024, "other", 1, 1, 1)); c2[0].QType != "other" {
		t.Error("其他题型")
	}
	mixed := structureOf(append(mk(2024, "other", 1, 1, 1), mk(2024, "calculation", 1, 1, 1)...))
	if mixed[0].QType != "calculation" || mixed[1].QType != "other" {
		t.Errorf("未列出顺序的题型按名称排：%+v", mixed)
	}
}

func TestMissingMaterialSections(t *testing.T) {
	ep := P.ExamProfile
	secs := []SectionCoverage{
		{SectionID: 1, Share: 0.3, KPCount: 40, Mastery: 50},  // 正常
		{SectionID: 2, Share: 0.2, KPCount: 5, Mastery: 50},   // 知识点少：平均 (40+5+30+40)/4=28.75，一半 14.4
		{SectionID: 3, Share: 0.15, KPCount: 30, Mastery: 10}, // 掌握度 < 20
		{SectionID: 4, Share: 0.05, KPCount: 40, Mastery: 0},  // 占比 < 10% 不提醒
	}
	if got := MissingMaterialSections(secs, ep); !reflect.DeepEqual(got, []int64{2, 3}) {
		t.Errorf("got %v", got)
	}
	if MissingMaterialSections(nil, ep) != nil {
		t.Error("空")
	}
}

// 排序的平局规则：结果确定、可复现。
func TestTieBreaks(t *testing.T) {
	ep := P.ExamProfile
	qs := []ExamQuestion{
		{Year: 2023, QType: QTermExplain, Score: 5, KPIDs: []int64{9, 3}},
		{Year: 2024, QType: QTermExplain, Score: 5, KPIDs: []int64{9, 3}},
	}
	p := BuildExamProfile(qs, ep)
	if len(p.HighFreq) != 2 || p.HighFreq[0].KPID != 3 || p.HighFreq[1].KPID != 9 {
		t.Errorf("次数相同按知识点 ID 升序：%v", p.HighFreq)
	}
	got := filterGroup([]PlanCandidate{
		{Group: GroupWeak, QuestionID: 8, Gain: 0.5},
		{Group: GroupWeak, QuestionID: 2, Gain: 0.5},
		{Group: GroupReview, QuestionID: 1, Gain: 0.9},
	}, GroupWeak)
	if len(got) != 2 || got[0].QuestionID != 2 {
		t.Errorf("收益相同按题目 ID 升序：%+v", got)
	}
}

func TestDayDateColumn(t *testing.T) {
	day := d(2026, 12, 20)
	if got := day.Date(); got != time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC) {
		t.Errorf("Date = %v", got)
	}
	if DayFromDateColumn(time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC)) != day {
		t.Error("DayFromDateColumn")
	}
}

func TestEstimateEssay(t *testing.T) {
	p := P.ScoreEstimate
	if _, ok := EstimateEssay(nil, 150, p); ok {
		t.Error("没有计入的作文不显示预估分")
	}
	// 1 篇 ±10%；最近 3 篇平均 ±4%，更早的不算。
	e, _ := EstimateEssay([]float64{100}, 150, p)
	if e.Low != 90 || e.High != 110 || e.BasisPapers != 1 {
		t.Errorf("1 篇：%+v", e)
	}
	e, _ = EstimateEssay([]float64{110, 100, 90, 30}, 150, p)
	if !near(e.Mid, 100) || e.Low != 96 || e.High != 104 || e.BasisPapers != 3 {
		t.Errorf("最近 3 篇：%+v", e)
	}
	e, _ = EstimateEssay([]float64{148, 146}, 150, p)
	if e.High != 150 {
		t.Errorf("截断在满分：%+v", e)
	}
	if w, ok := WeakestDimension([]DimScore{{"立意", 24, 30}, {"结构", 18, 30}, {"文采", 9, 15}, {"语言", 0, 0}}); !ok || w != "结构" {
		t.Errorf("得分率最低的维度：%s", w)
	}
}

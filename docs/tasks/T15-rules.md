# T15 规则包

- 仓库：后端（peetraining-server）
- 依赖：T03
- 计划完成：10/9
- 状态：审查中

## 目标

把 PRD v3 第 11 节写成可测试的纯函数。

## 范围

- internal/rules：掌握分（11.1）、掌握状态与以为会了（11.2）、复习间隔（11.3）、备考阶段与覆盖率（11.4）、今日计划（11.5）、预估分（11.6）、失分诊断（11.7）、错题本收录移出（11.8）、整卷时间（11.9）、AI 组卷选题（11.10）、考情统计（11.11）
- 参数全部来自 rule_params；每个函数注释写 PRD 节号
- 单元测试覆盖 PRD 中的每个例子与边界：没有真题时等权、只做过 1 套卷、资料缺题的卷子、多知识点题目、逾期衰减、阶段手动选择后不覆盖

## 参考

- docs/prd.md 第 11 节

## 验收

- make test 中 internal/rules 覆盖率 100%
- 提交说明附一张「规则 → 测试用例」对照表

## 不做

- 卡片范围以外的页面、接口和重构；发现需要的，记到 docs/open-questions.md

## 记录（开发中填写）

- 实现要点：
  - internal/rules 全部是纯函数，日期用 Day（北京时间自然日）；参数 Params 与 rule_params 的 JSON 键一一对应，ParseParams 用数据库值覆盖默认值
  - 集成测试保证迁移写入的 rule_params 与 DefaultParams 完全一致（用空结构体解码，漏字段也能发现）
  - 覆盖率 100%
- 偏离计划的地方与原因（PRD 未写明、按以下默认实现，已记 open-questions）：
  - 预估分区间 ±10% / ±6% / ±4% 以中值为基数（Q13）
  - 「试卷后段」= 按题序最后三分之一（Q12）
  - 失分诊断：采分点全部遗漏但 M ≥ 60 也归「答题不规范」（PRD 只写了部分命中）；不限时作答的空白答案归「知识没掌握」
  - 题型系数：真题里没出现过的题型取已出现题型里最小的系数
  - 单题用时参数里没有的题型（计算、其他）按论述 12 分钟计，宁可高估
  - 整卷建议用时参数补了客观题 2 分钟、作文 60 分钟（PRD 只给了名词解释、简答、论述）
  - AI 组卷「真题考过的知识点占 50–70%」取中值 60% 作为目标
- 规则 → 测试用例：

  | PRD | 函数 | 测试 |
  | --- | --- | --- |
  | 11.1 掌握分 | ApplyMastery、ApplyOverdue | TestApplyMasteryPRDTable（表内每一行、非主知识点减半、上下限、D16 自评）、TestApplyOverdue（逾期每天 −3、增量不重复扣） |
  | 11.2 掌握状态 | StateOf、TrimCorrectDays、IsCorrect、FalseMastery、SectionMastery | TestStateOf、TestTrimCorrectDays、TestIsCorrect、TestFalseMastery、TestSectionMastery |
  | 11.3 复习间隔 | NextReview、SubjectiveOutcome、ReciteOutcome | TestNextReview（3 → 7 → 15 → 30、模糊不前进、答错重置）、TestOutcomeMapping |
  | 11.4 备考阶段 | DefaultStage、Coverage、AdviseStage | TestDefaultStage（150 / 60 / 14 边界）、TestCoverage、TestAdviseStage（到点、提前冲刺、覆盖率低、手动选择不覆盖） |
  | 11.5 今日计划 | MixFor、KPWeight、QTypeCoef、Gain、ItemMinutes、BuildPlan | TestMixFor、TestKPWeightAndCoef（没有真题时等权）、TestItemMinutes、TestBuildPlan（多课平均、没题的课让出份额、同一知识点只出现一次、客观题在前、缺口） |
  | 11.6 预估分 | EstimateScore、ScalePaperScore | TestEstimateScore（没卷不显示、只做 1 套、3 套、近 20 题与不足 5 题、主要差在、截断）、TestScalePaperScore（缺题卷） |
  | 11.7 失分诊断 | ClassifyLoss、AggregateLoss | TestClassifyLoss（三类与时间判定的边界） |
  | 11.8 错题本 | WrongBookAdd、WrongBookProgress | TestWrongBook（收录三种情况、同日不重复、答错清零、2 个日期移出） |
  | 11.9 整卷时间 | SuggestedTimes、Overtime、TimeLoss、MockDeadline、CanResume、ShouldWarnTimeLeft | TestSuggestedTimes（654 示例）、TestPaperTimeHelpers |
  | 11.10 AI 组卷 | ComposePaper | TestComposeStandardPaper、TestComposeTargetedPaper（不重复知识点、不出做过的真题、针对卷避开近 30 天、缺口交给变式题） |
  | 11.11 考情统计 | BuildExamProfile、MissingMaterialSections | TestBuildExamProfile（回忆版与缺分值不计入、只有 1 套、结构近 N 年未变与变化年份、板块占比、高频考点）、TestMissingMaterialSections |
- 需要手动验证的步骤：无
- 验收结果：
  - [x] internal/rules 覆盖率 100%
  - [x] 规则 → 测试用例对照表见上

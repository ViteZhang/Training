# T10 AI 结构化：题目、采分点、知识点

- 仓库：后端（peetraining-server）
- 依赖：T09
- 计划完成：10/9
- 状态：审查中（真实模型样本测试留到最后统一做）

## 目标

把文字变成待确认的题目、采分点和知识点树，这是产品的第一步。

## 范围

- internal/ai：按能力配置模型、提示词版本、参数；JSON Schema 校验与重试；调用写 ai_calls；灰度路由（按用户比例）；写 ADR 0008
- 切题：规则按题号与题型标题切块，切不开的整页交给模型
- 题目结构化：题型、题干、选项、答案、分值、年份、出处页码、置信度
- 答案匹配：答案在单独文件或书后时按「年份 + 题号」跨文件配对
- 采分点提取（来自参考答案原文）；缺答案的题可按需生成参考答案与采分点（标 AI 生成）
- 参考资料类：知识点提取（原文表述必须能在资料里逐字找到）、按资料目录成树
- 题目类：按题目打知识点标签并成树；真题按年份组成真题卷（papers）
- 去重：内容哈希 + 相似度，标疑似重复
- 写入 import_items（待确认）；进度接口；部分完成可先确认
- cmd/eval：import 与 kp 两个评测，读 evals/private/ 的样本与标准答案，输出指标与是否达标

## 参考

- docs/dev-spec.md 第六节第 5–11 步、第七节
- docs/prd.md 第 12 节

## 验收

- 用 mock 模型跑通完整流水线；用真实模型对 3 份样本跑通
- make eval cap=import 能输出题干与答案识别正确率（门槛在 T31 达成，本卡只要求评测能跑）
- 任一步失败重跑不重复扣额度、不产生重复题目

## 不做

- 卡片范围以外的页面、接口和重构；发现需要的，记到 docs/open-questions.md

## 记录（开发中填写）

- 实现要点：
  - internal/ai 能力层（ADR 0008）：按能力配置模型档位、提示词版本（prompts/<能力>@<版本>.tmpl）、温度；ai_rollouts 灰度按用户分桶；JSON Schema + 能力校验，不合格重试一次，再失败 AIFailed；每次调用写 ai_calls（不存原文）；AI_PROVIDER=mock 时用规则 mock
  - 导入能力：import_structure（题干必须来自原文块）、rubric_extract（关键词必须在参考答案里）、answer_generate（采分点合计等于分值）、kp_extract（原文表述逐字可查）、kp_tag（复用已有路径成三级树）
  - internal/cloud/ai：OpenAI 兼容客户端，AI_PROVIDER=bailian 接百炼，AI_ALT_* 接对照平台
  - internal/importer 流水线：建任务时按已知页数预占解析额度（不足 402），取文本后按实际计费页数补预占；逐文件：取文本 → 内容安全 → 切题（规则按题号、题型标题、年份、参考答案部分切块，切不开的整页交模型）→ 结构化（或拆知识点）→ 结算额度（失败全部退回）；全部文件结束后整理：按年份 + 题号跨文件配答案、提取采分点、打知识点标签、与题库去重（内容哈希 + 相似度），进入待确认
  - 可重复执行：每步记 step，重试从没完成的一步开始；条目按内容哈希去重写入，重跑不重复；额度预占、结算按幂等键只生效一次；Asynq 重试用完时文件标失败并退回额度
  - 确认入库：同一事务扣导入题数并写题目、采分点、知识点树（同名节点复用）、题目知识点、知识点出处；按年份重组真题卷（满分取专业课满分，缺题记说明、按比例换算）；重算资料题数、知识点数与真题出现次数；可分批确认
  - 接口：createImportJob、listImportJobs、getImportJob（含各文件进度、识别条数、预计剩余时间）、retryImportMaterial、removeImportMaterial、listImportItems（筛选与分页）、getImportItem、updateImportItem（采分点合计校验 rubric_sum_mismatch）、generateImportItemAnswer（扣 AI 出题额度，失败不扣）、confirmImportJob
  - cmd/eval：make eval cap=import|kp，读 evals/private/*.jsonl，没有时用 evals/samples 的公开示例
- 偏离计划的地方与原因：
  - 迁移 00009 新增 import_answers（答案在单独文件时先存起来，整理时配对）与 import_job_materials 的预占记录
  - 失败退回按整份文件计（文件失败全部退回、成功按计费页数扣），没有做到单页粒度
  - 作文模式（mode=essay）在 T11 处理；今日计划在 T16 生成，plan_ready 暂为 false（AfterConfirm 钩子已留）
- 需要手动验证的步骤（最后统一做）：
  - 环境放开百炼域名并配置 BAILIAN_BASE_URL / BAILIAN_API_KEY 后，用 3 份真实样本走一遍导入并在确认页核对
  - 提供 evals/private/import.jsonl、kp.jsonl 后跑 make eval，结果记进 evals/README.md
- 验收结果：
  - [x] 用 mock 模型跑通完整流水线（TestQuestionImportPipeline、TestReferenceImport）
  - [ ] 用真实模型对 3 份样本跑通：等网络与样本
  - [x] make eval cap=import 能输出题干与答案识别正确率
  - [x] 任一步失败重跑不重复扣额度、不产生重复题目（重跑 ProcessMaterial / Finalize / Confirm 断言；失败文件退回额度 TestFailuresAndRefund）
  - [x] 拿别人的任务、条目、资料返回 404（TestImportOwnership）

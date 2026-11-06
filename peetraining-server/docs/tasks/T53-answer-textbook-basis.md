# T53 导入补答案：说明没用上教材的原因；按关联教材重新生成（11 月 6 日产品反馈）

- 仓库：后端 + 管理后台（云效 peetraining-admin）；App 不改（只同步接口契约副本）
- 依赖：T40、T49
- 状态：开发完成

## 背景

- 北师大 808 真题卷（项目 3）导入时勾了「缺答案时由 AI 生成」，63 道题的答案没有一条引用教材：项目没有关联教材，
  生成时直接按通用知识写了，核对区只显示「没有引用到项目关联的教材」，看不出原因。
- 先导入、后关联教材时，已经有 AI 答案的题不算「缺答案」，「一键补全」会跳过，没办法让它们用上教材。

需求见 [official-first.md「补答案与教材」](../features/official-first.md)。

## 范围 · 后端

- 答案生成（internal/ai GenerateReferenceAnswer、internal/textbook PrepareAnswer）记下没引用教材的原因：
  no_textbook 没关联教材 / unavailable 关联的都不能检索（逐本写：还没发布、授权不可用、还没有可检索的索引、索引未就绪等）/
  no_hits 没检索到段落 / not_relevant 段落与本题不相关 / not_cited 模型没引用 / citation_rejected 引用核对没通过 / retrieval_failed 检索出错。
  提示词、模型与检索规则不变
- 7.17 条目返回 answer_ai 与 answer_basis（原因、说明、逐本明细）；原因存在条目 payload 里，接受时去掉，不进草稿
- POST …/import-items/{itemId}/regenerate：重新生成一道题的 AI 答案（同步），失败时原答案不变、原因记在 fill_error
- POST …/imports/{fileId}/regenerate：整份重新生成，与一键补全共用 Worker 任务与进度；缺答案、缺采分点的题一起补
- 只换 AI 生成的答案；原文给的答案不动；原文给的采分点保留（payload.rubric_kept，T53 之前生成的题按 AI 采分点处理）
- 项目关联教材的 available 要求有发布的索引（current revision + generation），供上传前提醒用
- 不改库

## 范围 · 管理后台

- 上传时勾了「缺答案时由 AI 生成」、项目没有能检索的关联教材：先提醒「本项目还没有关联教材」/「关联的教材都还不能检索」，可以「仍然上传」或「先去关联」
- 核对区：AI 答案没引用教材时显示原因与逐本明细；AI 答案旁「按关联教材重新生成」；资料卡「按关联教材重新生成（N 题 AI 答案）」，确认后排队执行

## 验收

- [x] 没关联教材：AI 答案标 no_textbook
- [x] 关联了不能检索的教材：重新生成后标 unavailable，逐本写「还没发布」
- [x] 单题、整份重新生成：生成新的答案依据快照；原文采分点保留；原文答案不动、不能重新生成（400）；跨项目 404
- [x] 接受后草稿里没有 answer_basis、rubric_kept、regenerate，保留 answer_ai
- [x] 检索失败、引用核对没通过、模型没引用时原因正确（internal/ai 单元测试）
- [x] 后台上传提醒、核对区原因与重新生成按钮（peetraining-admin 测试）

## 需要在生产手动验证

1. 项目 3 关联《文学理论教程》《普通写作学教程》后，打开北师大 808 那份资料，点「按关联教材重新生成（63 题 AI 答案）」，等进度跑完
2. 文学理论相关的题应带教材引用；没引用的题看说明（多半是「没检索到相关段落」——古代汉语、现代汉语等教材库里还没有）

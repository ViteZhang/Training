# T49 7.17 解析后补答案与采分点；AI 参考答案计入预估分（11 月 4 日产品更新）

- 仓库：后端 + App（peetraining-web）+ 管理后台（云效 peetraining-admin）
- 依赖：T35、T41、T47
- 状态：开发完成（生产待验证）

## 背景

7.17 解析时自动给每道题生成答案，失败只记日志、置信度记低，核对页看不到答案和采分点，点「接受」又报「主观题要有采分点」；
名词解释没有分值时也补不上；几百道题一次归知识点，结果全是「未归类」。官方题库 AI 生成的答案发布给用户后显示成团队答案，与用户端的「AI 参考答案」口径不一致。

## 范围 · 后端

- 迁移 00042：official_import_files.auto_answer、一键补全进度（fill_status / fill_total / fill_done / fill_failed）；official_import_items.fill_error
- 上传时可勾「缺答案时由 AI 生成」（auto_answer，默认关）；不勾时解析不补答案
- POST /admin/official/projects/{id}/import-items/{itemId}/fill：单题「生成答案 / 提取采分点」；POST /admin/official/projects/{id}/imports/{fileId}/fill：整份一键补全（Worker 任务 official:import_fill，可重复执行、卡住一小时重新排队）
- 补全规则：缺答案的 AI 生成（主观题连同采分点，已填的采分点保留；客观题只要答案），标 payload.answer_ai，引用带书名、章节、原文、页码；有答案缺采分点的从答案提；主观题缺分值取同项目同题型最常见的分值（score_inferred），没有可参考的提示先填；失败原因记在条目上
- 接受时主观题没有采分点返回 needs_fill，提示先生成
- 归知识点改为每 30 题一批
- 官方题与用户副本记录答案来源（ai_generated / official），用户端档位为「AI 参考答案」；内容哈希只在 AI 答案时多算一项，已同步的副本不受影响
- AI 参考答案计入预估分：rules.CountsForEstimate 只排除缺答案；刷题、整卷不再给作答标 reference_only（体验题照旧）；批改结果只对以前标过的作答返回 reference_only；3.1 缺答案提醒不再算官方 AI 答案

## 范围 · App

- 去掉「AI 参考答案确认前不计入预估分」的说明（1.7、1.7b、3.1b、3.3b、4.7b）；以前标了不计入的作答仍显示「不计入预估分」

## 范围 · 管理后台

- 7.17 核对区：「生成答案 / 提取采分点」按钮、文件卡「一键补全」与进度、失败原因、AI 生成与分值待核对标记、引用原文与页码；上传时「缺答案时由 AI 生成」勾选；修改时可改分值与「答案由 AI 生成」

## 验收

- internal/practice/official_import_integration_test.go：TestOfficialImport（解析后待补、提取采分点、一键补全、跨项目 404）、TestOfficialImportFill（缺分值参考同题型、没有可参考的报错并记原因、客观题补答案、勾选后解析时补、发布后用户副本为 AI 参考答案且计入预估分、再次对账不误判）
- internal/practice/answer_integration_test.go：按 AI 参考答案批改的作答计入，以前标过的不追溯
- internal/rules：AI 参考答案计入预估分

## 不做

- 已发布官方题的历史答案来源回填（以前发布的都按团队答案处理）
- 提示词与模型不变，不需要重跑评测

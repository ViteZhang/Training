# T35 缺答案处理（PRD 10 月 26 日更新）

- 仓库：后端 + 前端（先后端，后前端）
- 依赖：T10、T11、T18、T21、T22、T34
- 计划完成：10/28
- 状态：开发完成（生产待验证）

## 目标

导入的题目缺答案时：先从用户其他资料匹配，再支持补充答案文件，最后做到时由 AI 给参考答案；AI 参考答案在用户确认前不计入预估分（PRD 11.16）。

## 范围 · 后端部分

- 迁移 00024：questions 加答案出处（answer_match、answer_material_id、answer_page）与 AI 答案的生成、确认 / 修改、报错记录；attempts.reference_only；import_jobs 加 answers 方式与 target_job_id；import_answers.stem；answer_pairings
- 档位规则 rules.TierOf：来自你的资料 / 你已确认 / AI 参考答案 / AI 出题自带答案 / 缺答案（D46）
- 导入第 7b 步：缺答案的题从同课其他资料匹配（D47），标「来自你的资料」并请核对（answer_matched）；1.7 统计与「缺答案」筛选
- 1.7c：POST /subjects/{id}/answer-files、GET / PUT /import-jobs/{id}/pairings、POST /import-jobs/{id}/pairings/confirm
- AI 参考答案：主观题提交、客观题作答、「看参考答案」遇到缺答案的题先生成并缓存（计入每日 AI 出题额度）；整卷批改时一次生成（计入整卷额度）；生成时带同课资料原文与参考教材（answer_generate@v2）
- 3.3b / 4.7b：POST /questions/{id}/answer/confirm；题目详情返回档位、出处、按 AI 答案批改的次数与得分；批改结果返回 reference_only、answer_confirmed
- 客观题 AI 答案被报错 3 次撤回
- 预估分：按 AI 参考答案判分的作答不计入模型分；整卷只用可信答案的题计分，一半以上主观题分值没有可信答案的真题卷暂不参与，卡片返回 pending_answer_papers 与 ai_confirmed_questions
- 3.1 提醒条 missing_answer_count；3.1b 按答案状态筛选；7.5 缺答案统计

## 验收

- 导入后从知识点、已有题目匹配到答案并记出处；答案文件按题号配对、可取消、确认后写入并提取采分点（importer 集成测试）
- 缺答案的主观题提交后先生成 AI 答案再批改、作答不计入预估分、确认后计入、之前不追溯；客观题同理；报错 3 次撤回；AI 出题额度用完 402（practice 集成测试）
- 越权：新接口拿别人的 ID 返回 404（IDOR 集成测试）
- 新提示词用 qwen3.8-flash 试跑虚构示例（evals/README.md 记录）

## 不做

- 「答案不完整」的识别：并入低置信度「需核对」
- 客观题 AI 答案的解析：只生成答案

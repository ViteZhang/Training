# 后端架构

本文给接手的工程师一张地图：模块怎么分、数据怎么流、核心表是什么。决策原因见 docs/adr/，业务规则见 docs/prd.md。

## 进程

```
App / 管理后台 ──HTTPS──> Nginx ──> api（Gin）──> MySQL
                                      │  └────> Redis（验证码、限频、Asynq 队列）
                                      └─入队─> worker（Asynq）──> MySQL / OSS / 百炼 / OCR / 内容安全
```

- api 与 worker 是同一个镜像的两个入口；单题批改（≤ 8 秒）、AI 解读（≤ 5 秒）由 api 直接调模型，资料解析、整卷批改、作文批改、导出、统计由 worker 执行
- 调度器（每日计划、注销到期删除、额度与统计汇总）跑在 worker 里，全局只有一个
- 客户端直传文件到 OSS 私有桶（预签名地址），api 不经手文件内容

## 代码结构

| 包 | 职责 |
| --- | --- |
| internal/app | 组装依赖、启动、优雅停机、迁移入口 |
| internal/http | 路由、中间件（请求 ID、访问日志、恢复、统一错误；鉴权、幂等、功能开关、审计在对应卡加入）、处理器 |
| internal/gen | oapi-codegen 按 api/openapi.yaml 生成的接口与类型 |
| internal/dbq | sqlc 按 db/queries 生成的查询代码 |
| internal/store | MySQL、Redis 连接，goose 迁移 |
| internal/jobs | Asynq 任务、处理器、定时任务 |
| internal/cloud | 外部服务接口与 mock |
| internal/rules | 业务规则纯函数（PRD 11 节），参数读 rule_params（T15） |
| internal/ai | 按能力的提示词、校验、重试、灰度（T10） |
| internal/<业务域> | auth、profile、bank、material、importer、practice、grading、paper、recite、essay、plan、score、quota、membership、payment、official、admin、notify、export |

## 数据模型

一切内容挂在题库下：用户 → 专业课 → 题库 → 知识点与题目。迁移文件在 db/migrations，按领域分组：

| 迁移 | 领域 | 主要表 |
| --- | --- | --- |
| 00002 | 账号与档案 | users、refresh_tokens、agreements、agreement_acceptances、exam_dates、study_profiles、subjects |
| 00003 | 题库内容 | banks、materials、material_pages、import_jobs、import_job_materials、import_items、knowledge_points、kp_sources、kp_relations、questions、rubric_points、question_kps、question_reports、papers、paper_questions |
| 00004 | 学习 | practice_sessions、paper_sessions、paper_session_items、attempts、gradings、disputes、kp_mastery、wrong_book、daily_plans、recite_records、score_estimates |
| 00005 | 作文 | essay_rubrics、essays、writing_methods、essay_materials、model_essays |
| 00006 | 商业化 | quota_counters、quota_ledger、memberships、redeem_batches、redeem_codes、orders、refunds、invites、survey_responses |
| 00007 | 运营与配置 | rule_params、feature_flags、app_versions、messages、announcements、export_jobs、feedbacks、admin_*、content_access_*、ai_calls、ai_rollouts、stats_hourly、official_*、bank_subscriptions |
| 00008 | 初始配置 | 规则参数、功能开关（默认关）、2027 初试日期、通用作文评分标准 |

几条贯穿全局的约定：

- 时间 DATETIME(3) 存 UTC；业务日期（plan_date、next_review_on、额度周期）按北京时间
- 用户内容表带 owner_user_id，对 users 外键级联删除（ADR 0006）
- 题目、知识点、采分点等 AI 产物带 origin；批改记采分点快照（ADR 0005）
- 额度：quota_counters 按（用户、类型、周期）计数，扣减时行锁；quota_ledger 记流水，幂等键防重复扣
- 写接口的幂等键存在对应业务表的 (owner_user_id, idempotency_key) 唯一索引上
- 中文全文搜索用 InnoDB FULLTEXT + ngram 解析器（material_pages、knowledge_points、questions）

## 导入数据流（dev-spec 第六节）

1. App 申请上传 → 拿到预签名地址直传 OSS → 回调确认（materials）
2. 创建导入任务，按计费页数预占额度（quota_counters.reserved）
3. worker 逐文件跑流水线：取文本 → 内容安全 → 切题 → AI 结构化 → 配答案 → 采分点 → 知识点标签 → 去重，每步状态记 import_job_materials，失败只重跑这一步
4. 产出写 import_items（待确认）；用户在 1.7 / 1.7b 修改、确认
5. 确认后写正式表（questions、rubric_points、knowledge_points、papers），结算额度（失败页退回），生成今日计划

## 接口

- 契约：api/openapi.yaml；当前版本见文件头部说明
- 未实现的接口返回 501（stubs_gen.go 自动生成）

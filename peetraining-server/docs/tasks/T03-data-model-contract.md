# T03 数据模型 v1 与接口契约 v1

- 仓库：后端（peetraining-server）
- 依赖：T01
- 计划完成：10/6
- 状态：审查中

## 目标

一次定下全部表结构和全量接口契约，前后端之后按它并行开发。

## 范围

- 按 docs/dev-spec.md 第五节两张表写 goose 迁移：账号、备考档案、题库、资料、导入、知识点、题目、试卷、作答、学习状态、整卷与估分、商业化、配置、后台、AI 账本、作文、知识关联、支付、增长、消息与导出、官方题库
- 所有用户内容表带 owner_user_id 与必要索引；时间字段 DATETIME(3) 存 UTC
- 契约分批：本卡先写模块 0、1、3 与导入相关接口打 api-v0.1，让前端开工；其余模块由对应卡增量加入并打新标签，10/11（G2）冻结 api-v1.0。最终 api/openapi.yaml 覆盖 PRD v3 模块 0–7 的全部接口（按 tag 分组：auth、profile、bank、import、practice、grading、paper、recite、essay、plan、score、membership、payment、message、export、official、admin），每个字段写说明
- 错误码表；分页、幂等、功能开关关闭时返回 404 的约定写进契约
- make gen 生成 Gin 接口与类型；未实现的处理器返回 501
- make api-tag 打 api-v0.1 标签（只含本卡这一批）
- 写 ADR 0004–0006；docs/architecture.md 初版（模块划分、数据流、核心表）

## 参考

- docs/dev-spec.md 第五节、第六节
- docs/prd.md 第 6–10 节、第 11 节、第 13 节

## 验收

- make migrate-up 在空库上成功；再执行一次无变化
- openapi.yaml 通过校验；前端 pnpm gen:api 能用 api-v0.1 生成客户端
- 模块 0、1、3 与导入的每个页面需要的数据都能在 api-v0.1 里找到对应接口（提交说明附页面 → 接口对照表；其余模块的对照表随各卡补齐）

## 不做

- 卡片范围以外的页面、接口和重构；发现需要的，记到 docs/open-questions.md

## 记录（开发中填写）

- 实现要点：
  - 迁移 00002–00008 按领域分组，覆盖 dev-spec 第五节两张表的全部领域；00008 写入 PRD 第 11、13 节的规则参数初始值、6 个默认关闭的功能开关、2027 初试日期、通用五维度作文标准
  - 用户内容表都带 owner_user_id 并对 users 外键级联删除；题目删除时作答、批改、错题级联删除，整卷成绩保留（集成测试 TestSchemaCascades）
  - 迁移可从空库执行、整体回滚、再执行（TestMigrationsUpDownUp），重复执行无变化
  - sqlc 接入（sqlc.yaml → internal/dbq，JSON 列映射为 json.RawMessage）；工具统一放 tools/go.mod
  - 契约 api-v0.1：模块 0、1、3 与导入、额度、功能开关共 57 个接口；错误码、分页游标、Idempotency-Key、功能开关关闭返回 404 写在契约说明里；测试校验契约是合法 OpenAPI 3
  - stubgen：契约里有、还没实现的接口自动生成返回 501 的占位处理器，实现后自动消失（make gen 执行）
  - 新增错误码 AI_FAILED（502）
  - ADR 0004–0006；docs/architecture.md 初版
- 偏离计划的地方与原因：
  - 契约按 D17 分批：本卡只出 api-v0.1，其余模块随卡增量
  - 表名 daily_plans 的计划内容列叫 plan_groups（groups 是 MySQL 保留字）
  - 「试卷后段」的定义 PRD 未写明，rule_params.loss_diagnosis.tail_start_ratio 先取 0.67（按题序最后三分之一），已记 open-questions Q12
- 页面 → 接口对照（api-v0.1）：

  | 页面 | 接口 |
  | --- | --- |
  | 0.1 启动页、0.4b、0.6、0.6b | GET /bootstrap |
  | 0.2、0.2a、0.2b、0.3、0.3b、0.3c | POST /auth/sms-codes、POST /auth/login |
  | 0.4 协议与政策 | GET /agreements/{kind} |
  | 0.4b 同意并继续 | POST /me/agreements |
  | 令牌续期、退出登录（6.10） | POST /auth/refresh、POST /auth/logout |
  | 6.11 账号与安全 | GET /me、GET /me/devices、DELETE /me/devices/{deviceId}、POST /auth/sms-codes（change_phone_*）、PUT /me/phone |
  | 6.12 注销账号 | POST /me/deletion、DELETE /me/deletion |
  | 1.1 你考哪门专业课 | GET /exam-years、GET/POST /subjects、PATCH /me（onboarding_step） |
  | 1.2 设定目标分、2.1f | PATCH /subjects/{subjectId} |
  | 1.3 备考安排、6.9 | GET/PUT /profile、PATCH/DELETE /subjects/{subjectId} |
  | 1.4 选择导入方式 | GET /subjects、GET /quota |
  | 1.5 选择文件 | POST /materials/upload-requests、POST /materials/{materialId}/uploaded |
  | 1.5b 粘贴文字 | POST /materials/paste |
  | 1.6 AI 解析中 | POST /import-jobs、GET /import-jobs/{jobId}、PUT /profile（reminder_times） |
  | 1.6b 部分文件识别失败 | POST /import-jobs/{jobId}/materials/{materialId}/retry、DELETE /import-jobs/{jobId}/materials/{materialId} |
  | 1.7 确认导入结果 | GET /import-jobs/{jobId}/items、PATCH /import-items/{itemId}、POST /import-jobs/{jobId}/confirm、PATCH /subjects/{subjectId}（is_essay） |
  | 1.7b 核对采分点 | GET/PATCH /import-items/{itemId}、POST /import-items/{itemId}/generate-answer、GET /materials/{materialId}/pages/{pageNo} |
  | 1.8 题库建好了 | POST /import-jobs/{jobId}/confirm 的结果、GET /subjects/{subjectId}/bank |
  | 2.1b 题库整理中、2.1c 还没导入 | GET /import-jobs?active=true、GET /subjects |
  | 3.1 知识点 | GET /subjects/{subjectId}/knowledge-tree、GET /subjects/{subjectId}/bank、POST /subjects/{subjectId}/knowledge-points |
  | 3.1b 题目 | GET /subjects/{subjectId}/questions、POST /subjects/{subjectId}/questions |
  | 3.1c 资料、6.3 我的资料 | GET /subjects/{subjectId}/materials、GET /quota |
  | 3.1d 删除资料确认 | GET /materials/{materialId}/deletion-impact、DELETE /materials/{materialId} |
  | 3.2 搜索 | GET /subjects/{subjectId}/search |
  | 3.3 题目详情 | GET/PATCH/DELETE /questions/{questionId} |
  | 3.4 知识点卡片 | GET /knowledge-points/{kpId}、PUT /knowledge-points/{kpId}/self-assessment |
  | 3.5 更多操作 | POST /knowledge-points/{kpId}/merge、/split、/explanation、DELETE /knowledge-points/{kpId} |
  | 3.6 编辑知识点 | PATCH /knowledge-points/{kpId} |
  | 3.7 原文查看 | GET /materials/{materialId}/pages/{pageNo} |
  | 功能开关 | GET /feature-flags（也包含在 /bootstrap） |

  3.8–3.10 在 T14 加入契约。
- 需要手动验证的步骤：无（全部由集成测试覆盖）
- 验收结果：
  - [x] make migrate-up 在空库上成功；再执行一次无变化
  - [x] openapi.yaml 通过校验；pnpm gen:api 生成客户端在 T02 验证
  - [x] 模块 0、1、3 与导入的页面 → 接口对照表见上

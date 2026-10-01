# T03 数据模型 v1 与接口契约 v1

- 仓库：后端（peetraining-server）
- 依赖：T01
- 计划完成：10/6
- 状态：未开始

## 目标

一次定下全部表结构和全量接口契约，前后端之后按它并行开发。

## 范围

- 按 docs/dev-spec.md 第五节两张表写 goose 迁移：账号、备考档案、题库、资料、导入、知识点、题目、试卷、作答、学习状态、整卷与估分、商业化、配置、后台、AI 账本、作文、知识关联、支付、增长、消息与导出、官方题库
- 所有用户内容表带 owner_user_id 与必要索引；时间字段 DATETIME(3) 存 UTC
- api/openapi.yaml 覆盖 PRD v3 模块 0–7 的全部接口（按 tag 分组：auth、profile、bank、import、practice、grading、paper、recite、essay、plan、score、membership、payment、message、export、official、admin），每个字段写说明
- 错误码表；分页、幂等、功能开关关闭时返回 404 的约定写进契约
- make gen 生成 Gin 接口与类型；未实现的处理器返回 501
- make api-tag 打 api-v0.1 标签
- 写 ADR 0004–0006；docs/architecture.md 初版（模块划分、数据流、核心表）

## 参考

- docs/dev-spec.md 第五节、第六节
- docs/prd.md 第 6–10 节、第 11 节、第 13 节

## 验收

- make migrate-up 在空库上成功；再执行一次无变化
- openapi.yaml 通过校验；前端 pnpm gen:api 能用 api-v0.1 生成客户端
- PRD v3 每个页面需要的数据都能在契约里找到对应接口（在提交说明里附一张页面 → 接口对照表）

## 不做

- 卡片范围以外的页面、接口和重构；发现需要的，记到 docs/open-questions.md

## 记录（开发中填写）

- 实现要点：
- 偏离计划的地方与原因：
- 需要手动验证的步骤：

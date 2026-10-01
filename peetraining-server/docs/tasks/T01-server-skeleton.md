# T01 后端工程骨架

- 仓库：后端（peetraining-server）
- 依赖：—
- 计划完成：10/5
- 状态：未开始

## 目标

建立可运行、可测试、可检查的 Go 工程，后面每张卡都在它上面加功能。

## 范围

- Go 模块与目录按 CLAUDE.md「目录」建立；cmd/api 与 cmd/worker 同一程序两种启动方式
- 配置从环境变量读取，提供 .env.example（只放占位值）
- log/slog JSON 日志；请求 ID 中间件；统一错误响应 { code, message, detail }
- GET /api/v1/health 返回版本号、数据库与 Redis 连通性
- Makefile：dev、gen、test、lint、migrate-new、migrate-up、eval；golangci-lint 配置
- docker-compose.dev.yml 起本地 MySQL 8 与 Redis；testcontainers-go 集成测试样例
- internal/cloud 下短信、AI、OCR、语音、内容安全、支付的接口定义与 mock 实现，按环境变量切换
- 优雅停机；Dockerfile 多阶段构建
- 写 ADR 0001–0003；README.md 写本地启动步骤

## 参考

- docs/tech-plan.md「选型」「架构」
- docs/dev-spec.md 第四节

## 验收

- make dev 后 curl localhost/api/v1/health 返回 200 且显示数据库、Redis 正常
- make lint、make test 通过，包含一个连真实 MySQL 的集成测试
- 所有云服务在本地默认走 mock，不需要任何真实密钥就能启动

## 不做

- 卡片范围以外的页面、接口和重构；发现需要的，记到 docs/open-questions.md

## 记录（开发中填写）

- 实现要点：
- 偏离计划的地方与原因：
- 需要手动验证的步骤：

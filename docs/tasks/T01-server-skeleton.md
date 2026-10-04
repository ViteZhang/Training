# T01 后端工程骨架

- 仓库：后端（peetraining-server）
- 依赖：—
- 计划完成：10/5
- 状态：审查中

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
  - 三个入口 cmd/api、cmd/worker、cmd/eval 共用 internal/app；`api migrate up|status|new` 负责迁移，迁移文件内嵌进二进制，流水线直接用镜像执行
  - api/openapi.yaml 先只有 GET /api/v1/health，oapi-codegen 生成 Gin 接口（枚举常量带类型前缀）；T03 在此基础上扩展
  - 统一错误 { code, message, detail } 与错误码在 internal/http/errors.go；处理器 `c.Error(err)` 即可；未知错误一律 500 且不返回内部信息；panic 转 500
  - 请求 ID（沿用合法的 X-Request-ID）进日志；访问日志只记路由模板，不记查询参数与请求体；logx 把日志中的手机号自动脱敏
  - 健康检查并发 Ping MySQL 与 Redis（2 秒超时），只返回 ok / error，任一异常返回 503
  - MySQL 连接强制 parseTime、会话时区 +00:00、utf8mb4_0900_ai_ci
  - internal/cloud 六个服务（短信、AI、OCR、语音、内容安全、支付）的接口与 mock；未实现的服务商启动即报错；生产环境禁止 mock 短信
  - Asynq：critical / default / low 三个队列；调度器跑在 Worker 里、时区 Asia/Shanghai；ping 任务用于检查链路
  - 集成测试用 testcontainers（internal/testenv），每个测试独占数据库；没有 Docker 时跳过，设 REQUIRE_INTEGRATION=1 强制
  - 依赖与理由：gin（路由，CLAUDE.md 选型）、go-sql-driver/mysql、go-redis/v9、asynq、goose/v3（迁移）、oapi-codegen/runtime（生成代码运行时）、testcontainers-go（集成测试）；工具 oapi-codegen、golangci-lint 用 go.mod 的 tool 指令锁版本
- 偏离计划的地方与原因：
  - 没有用 goose 命令行，改为 `api migrate` 子命令：goose 命令行会把所有数据库驱动都编译进来，且生产镜像里本来就需要能执行迁移
  - Dockerfile 的基础镜像与 GOPROXY 做成构建参数（默认 goproxy.cn）：云效在国内构建，docker.io / gcr.io / proxy.golang.org 可能拉不到，T04 配流水线时换成 ACR 同步镜像
  - golangci-lint 用 v2.14.0：v2.5.0 不支持 Go 1.27 标准库
- 需要手动验证的步骤：
  - 本机 `make dev` 后 `curl localhost:8080/api/v1/health` 返回 200（已在开发环境验证）
  - 镜像构建在开发环境因 Docker Hub 限流未能验证（`make build` 静态二进制已验证）；T04 在云效流水线里验证 `make docker`
- 验收结果：
  - [x] make dev 后 curl /api/v1/health 返回 200，mysql、redis 均为 ok
  - [x] make lint（0 issues）、make test 通过，含连真实 MySQL 的集成测试（健康检查、迁移执行两次、Worker 处理任务、优雅停机）
  - [x] 所有云服务本地默认 mock，不设任何环境变量即可启动

# peetraining-server

考研Training 后端：Go API + Worker。项目背景、规矩与文档优先级见 [CLAUDE.md](CLAUDE.md)；
功能与业务规则见 [docs/prd.md](docs/prd.md)；技术方案与开发计划见 [docs/dev-spec.md](docs/dev-spec.md)。

## 本地启动

需要：Go 1.25.3（`go.mod` 已按 Go 1.25 锁定依赖，不会自动下载其他工具链），以及 ECS 上 MySQL、Redis 对你本机的可达地址。

```bash
cp .env.local.example .env.local   # 填入 training_dev 数据库密码和 ECS 地址
chmod 600 .env.local
make dev          # 直连 ECS 数据库并启动本地 API（:8080）；Ctrl+C 停止
curl localhost:8080/api/v1/health
```

脚本直接连接 `.env.local` 中配置的 ECS MySQL 和 Redis。建议为本地开发单独创建 `training_dev` 数据库；`.env.local` 指向它，生产 `.env` 使用 `training`。直连模式下，ECS 安全组必须只允许你的办公公网 IP 访问 3306 和 6379，不能对全网开放。

本地 API 写入独立的 `training_dev` 库；Redis 使用逻辑库 DB 2，生产使用 DB 1，现有服务默认 DB 0。短信、AI、OCR、语音、内容安全、支付和 OSS 默认走 mock。默认只启动 API，不执行迁移，也不启动 Worker。`make dev-worker` 会额外启动本地 DB 2 队列的 Worker；只有明确需要时才使用。

本地不安装 Docker，也不启动第二套 MySQL / Redis。先在 ECS 上为本地开发创建独立的 `training_dev` 数据库和账号，再把 [db/init.sql](/Users/a1/Documents/workspace/myself/peetraining-server/db/init.sql) 手动执行到它。该文件包含全部 18 个迁移的 Up SQL、内置配置种子和 Goose 版本记录。生产 `training` 库已提前初始化，云效发布不会运行数据库迁移；不要把本地 SQL 执行到现有 `dreamer_ai` 库。

执行完 SQL 后，复制 `.env.local.example` 为 `.env.local`，填好 ECS MySQL/Redis 地址和密码，再运行 `make dev`。需要处理异步题库或批改任务时，另开终端运行 `make dev-worker`；它会使用 Redis DB 2。`make migrate-status` 和 `make migrate-up` 仅用于后续新增迁移，不要在已经执行 `db/init.sql` 的库上重复执行旧迁移。

## 查看 OpenAPI 和调试接口

GoLand 运行 `cmd/api` 时，在运行配置中设置 `APP_ENV=local` 并加载本地环境变量；程序自身不会自动读取 `.env.local`。
重启服务后打开 [Swagger UI](http://127.0.0.1:8080/docs)，展开接口，点击 **Try it out → Execute** 即可调用本机 API。
先验证 `GET /health`（200，MySQL/Redis 均为 ok），再验证 `GET /bootstrap`（platform=ios、app_version=1.0.0）。
需要登录的接口通过 **Authorize** 填写访问令牌；写接口会操作当前配置的数据库。

契约源文件是 `api/openapi.yaml`，本地在线 JSON 是 `http://127.0.0.1:8080/openapi.json`，均可导入 Apifox/Postman。
`/docs` 和 `/openapi.json` 仅在 `APP_ENV=local` 开放。Swagger UI 的 JS/CSS 从固定版本的 CDN 加载；如果 CDN 不可达，
可在 GoLand 打开 `api/local.http`，点击请求左侧的运行按钮进行验证。

## 常用命令

| 命令 | 作用 |
| --- | --- |
| `make gen` | 按 `api/openapi.yaml` 生成 `internal/gen`（改契约后必须执行） |
| `make test` | 全部测试，含用 testcontainers 起真实 MySQL / Redis 的集成测试；没有 Docker 时集成测试跳过，流水线设 `REQUIRE_INTEGRATION=1` 强制执行 |
| `make test-unit` | 只跑单元测试 |
| `make lint` | gofmt、go vet、golangci-lint |
| `make migrate-new name=add_users` | 新建顺序编号的迁移文件 |
| `make migrate-up` / `make migrate-status` | 连接 `.env.local` 配置的数据库执行迁移 / 查看状态 |
| `make build` / `make docker` | 构建二进制 / 镜像 |
| `make eval cap=grading` | 跑 AI 评测（各项在对应任务卡里实现） |
| `make api-tag v=api-v0.1` | 给接口契约打标签，供前端拉取 |

工具（oapi-codegen、sqlc、golangci-lint）放在独立的 `tools/go.mod` 里锁定版本，用 `go tool -modfile=tools/go.mod` 运行（Makefile 已封装），不用另外安装，也不会进入应用依赖。

## 程序入口

| 入口 | 说明 |
| --- | --- |
| `api` | HTTP 服务；`api migrate up\|status` 用于显式执行或查看数据库迁移 |
| `worker` | Asynq 任务消费与定时任务；调度器全局只能有一个实例 |
| `eval` | AI 评测命令 |

api 与 worker 打在同一个镜像里（`/app/api`、`/app/worker`）。收到 SIGTERM 后优雅停机：
API 不再接新请求、等进行中的请求完成；Worker 停止调度、等进行中的任务完成，没完成的回到队列重试。
等待上限由 `SHUTDOWN_TIMEOUT` 控制（默认 30 秒）。

## 环境变量

完整列表与说明见 [.env.example](.env.example)。生产环境（`APP_ENV=production`）必须显式设置
`MYSQL_DSN`、`REDIS_ADDR`、`JWT_SECRET`（至少 32 个字符），且不能使用 mock 短信，否则启动失败。

## 目录

```
cmd/            api、worker、eval 三个入口
api/            openapi.yaml 接口契约与 oapi-codegen 配置
internal/app    组装依赖、启动与停机
internal/config 环境变量配置
internal/http   路由、处理器、中间件、错误码（errors.go）
internal/gen    生成代码，禁止手改
internal/store  MySQL、Redis、迁移
internal/jobs   Asynq 任务与定时任务
internal/cloud  外部服务接口与 mock
internal/logx   JSON 日志（手机号自动脱敏）
internal/testenv 集成测试用的 MySQL / Redis 容器
db/migrations   goose 迁移（内嵌进二进制）
docs/           PRD、研发规格、任务卡、ADR
```

## 发布

见 [docs/runbook.md](docs/runbook.md)：首次搭建、发布与故障排查（`deploy/scripts/release.sh`）。

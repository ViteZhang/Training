# peetraining-server

考研Training 后端：Go API + Worker。项目背景、规矩与文档优先级见 [CLAUDE.md](CLAUDE.md)；
功能与业务规则见 [docs/prd.md](docs/prd.md)；技术方案与开发计划见 [docs/dev-spec.md](docs/dev-spec.md)。

## 本地启动

需要：Go 1.27（`go.mod` 声明了 1.27，旧版本 Go 会自动下载 1.27 工具链）、Docker。

```bash
make dev          # 起本地 MySQL 与 Redis → 执行迁移 → 同时启动 API（:8080）与 Worker；Ctrl+C 停止
curl localhost:8080/api/v1/health
make dev-down     # 停掉本地 MySQL 与 Redis
```

本地不需要任何真实密钥：短信、AI、OCR、语音、内容安全、支付默认都走 mock（`internal/cloud`）。

## 常用命令

| 命令 | 作用 |
| --- | --- |
| `make gen` | 按 `api/openapi.yaml` 生成 `internal/gen`（改契约后必须执行） |
| `make test` | 全部测试，含用 testcontainers 起真实 MySQL / Redis 的集成测试；没有 Docker 时集成测试跳过，流水线设 `REQUIRE_INTEGRATION=1` 强制执行 |
| `make test-unit` | 只跑单元测试 |
| `make lint` | gofmt、go vet、golangci-lint |
| `make migrate-new name=add_users` | 新建顺序编号的迁移文件 |
| `make migrate-up` / `make migrate-status` | 对本地库执行迁移 / 查看状态 |
| `make build` / `make docker` | 构建二进制 / 镜像 |
| `make eval cap=grading` | 跑 AI 评测（各项在对应任务卡里实现） |
| `make api-tag v=api-v0.1` | 给接口契约打标签，供前端拉取 |

工具（oapi-codegen、sqlc、golangci-lint）放在独立的 `tools/go.mod` 里锁定版本，用 `go tool -modfile=tools/go.mod` 运行（Makefile 已封装），不用另外安装，也不会进入应用依赖。

## 程序入口

| 入口 | 说明 |
| --- | --- |
| `api` | HTTP 服务；`api migrate up\|status` 执行迁移（发布流水线在启动新版本前执行 up） |
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

见 [docs/runbook.md](docs/runbook.md)：首次搭建、发布与回滚（`deploy/scripts/release.sh`、`rollback.sh`）、备份恢复、故障排查。

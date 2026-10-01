# 基础技术方案

本文是 2026 年 9 月 28 日技术方案的摘要，已按 10 月 1 日的决定更新。技术方案的调整、数据模型、导入引擎与开发顺序见 docs/dev-spec.md；功能与业务规则见 docs/prd.md。

## 选型

| 层 | 选型 | 运行在 |
| --- | --- | --- |
| 移动端 | Expo SDK 57 + TypeScript + Expo Router，development build | EAS 打包 |
| 管理后台 | React + Vite + TypeScript + React Router + Ant Design | 应用机 Nginx，training.dreamelab.cn/admin/ |
| 接口契约 | OpenAPI 3，契约先行，后端仓库维护 | — |
| API 服务 | Go 1.27 + Gin | ECS · Docker Compose |
| 后台任务 | Go Worker + Asynq（Redis 队列与定时） | ECS · Docker Compose |
| 数据库 | MySQL 8 自建；sqlc + go-sql-driver/mysql；迁移 goose | 数据库机 · Docker |
| 队列与缓存 | Redis 自建，开 AOF | 数据库机 · Docker |
| 文件 | 阿里云 OSS 私有桶，预签名直传，服务端加密 | OSS |
| 登录 | 手机号 + 短信验证码（阿里云短信），后端自建令牌 | API |
| AI | 阿里云百炼（OpenAI 兼容接口）为主，另一家国内平台对照 | 云端 API |
| 识别与审核 | 阿里云文字识别、智能语音交互、内容安全 | 云端 API |
| 支付 | 微信支付、支付宝 App 支付；App Store 内购（默认关闭） | 云端 API |
| 代码与发布 | 云效代码库 + 云效流水线 → 容器镜像服务 ACR → ECS | — |
| 日志与监控 | SLS 日志服务 + 云监控 | — |

全部资源在阿里云华北 2（北京），与百炼同地域。

## 架构

- App 和管理后台只调用 Go API；数据库、存储、模型和第三方服务都在 API 后面，客户端不持有任何云服务密钥
- 应用机（4 核 16G）：nginx（HTTPS、反向代理、托管后台网页）、api、worker、asynqmon（仅内网）
- 数据库机：mysql、redis，安全组只放行应用机内网访问；MySQL 数据放独立数据盘，开自动快照和云盘加密
- 对外只有一个域名 training.dreamelab.cn：App 调 /api/v1/，后台接口 /api/v1/admin/，后台网页 /admin/
- 耗时工作（资料解析、整卷批改、作文批改、导出、统计汇总）由 API 写入 Asynq 队列，Worker 执行；单题批改（≤ 8 秒）和 AI 解读（≤ 5 秒）由 API 直接调用模型
- API 与 Worker 是同一个 Go 程序的两种启动方式

## 仓库与协作

| 仓库 | 内容 |
| --- | --- |
| peetraining-server | cmd/api、cmd/worker、cmd/eval；internal 按业务域分包；api/openapi.yaml；db/migrations 与 db/queries；deploy/；evals/ |
| peetraining-web | pnpm 工作区：apps/mobile、apps/admin、packages/api-client（生成）、packages/ui-tokens |

接口流程（契约先行）：

1. 新增或修改接口先改 openapi.yaml
2. 后端用 oapi-codegen 生成 Gin 路由接口和类型，只写实现；合并后打标签，如 api-v0.3
3. 前端按标签拉取 openapi.yaml，生成 packages/api-client；后端未完成时用契约起 mock 服务开发
4. 后端流水线检查契约破坏性变更（oasdiff），有则必须升版本或兼容旧 App

约定：接口前缀 /v1；错误统一返回 code、message、detail；写接口带 idempotency_key；数据库时间存 UTC，业务日期按北京时间；已发布的 App 不能马上升级，后端改接口要兼容至少上一个版本，或配合强制更新。

## 环境与发布

- 只有本地和生产两套环境。本地用 docker compose 起 MySQL 与 Redis，短信、AI、OCR、支付默认 mock
- 生产：training.dreamelab.cn，内测即生产。补位规矩：每次发布前备份数据库；迁移只加不删，删除字段分两次发布；新功能先用功能开关对自己的账号打开；内测用户 50 人以内
- 发布流程：跑测试 → 构建镜像并打版本号 → 推送 ACR → 备份数据库 → 应用机执行迁移 → docker compose pull 与 up；回滚把镜像版本号改回上一个；后台网页由流水线构建后同步到 Nginx 目录
- 备份：MySQL 每日全量备份并持续上传 binlog 到 OSS，保留 30 天；云盘每日快照；每月用备份在本地做一次恢复演练

## 交接要求

代码由 Claude Code 编写，以后由技术合伙人维护。交付标准：没参与开发的工程师拿到仓库就能接手。每个仓库必须有：CLAUDE.md、README.md（本地启动、环境变量、测试、发布）、docs/architecture.md（模块、数据流、核心表）、docs/adr/（每个重要决定一篇）、docs/runbook.md（后端：发布、回滚、备份恢复、故障排查）。账号与密钥清单单独保管和移交，不进仓库。

## 合规

| 事项 | 何时 |
| --- | --- |
| ICP 备案 | 已有（公司备案主体与域名，接入商阿里云） |
| 隐私政策、用户协议、会员服务协议；App 内注销 | 内测前 |
| 内容安全审核；AI 生成内容标识 | 内测前 |
| App 备案（包名 cn.dreamelab.training 与签名证书定死后） | 正式上架前 |
| 软件著作权 | 上架国内安卓商店前 |
| 生成式 AI 应用登记，公示模型名称与备案号；是否需要算法备案一并咨询 | 面向公众正式上线前（内测是否需要，10 月 20 日前问清） |

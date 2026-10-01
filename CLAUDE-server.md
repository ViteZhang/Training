# Training 后端（peetraining-server）· 项目上下文

考研Training：考研文科专业课的 AI 提分教练。考生把自己的专业课资料和题目导进来，AI 解析成个人题库，按采分点批改主观题、安排每日复习，并依据考生自己导入的真题估算专业课分数。
- 自建题库为主，不限院校；只做名词解释、简答、论述为主的文科专业课，不做公共课
- 官方题库是同一套题库模型里的第二种来源（source = official），默认关闭
- 项目代号 Training（原「刷题精灵」）；App 显示名「考研Training」，由配置读取，不写死

本仓库是 Go 后端（API + Worker）。App 与管理后台在 peetraining-web 仓库，两边只通过 api/openapi.yaml 对接。

## 依据文档（冲突时从上到下取）

1. docs/prd.md：PRD v3。功能范围、逐页规则、业务规则（第 11 节）、AI 能力与评测门槛（第 12 节）、额度与会员（第 13 节）
2. docs/dev-spec.md：研发规格与开发计划。技术方案调整、数据模型（第五节）、导入引擎（第六节）、后台约束（第十节）、任务卡顺序与关口（第十二节）
3. docs/tech-plan.md：基础技术方案（选型、部署、发布、交接）
4. docs/tasks/：任务卡，一张卡一个文件；docs/open-questions.md：未决问题；docs/adr/：技术决策记录；docs/runbook.md：运维手册

文档没写清的地方：先追加到 docs/open-questions.md，再停下来问；不要自己发明业务规则。

## 技术栈

- Go 1.27；Gin；接口代码由 oapi-codegen 按 api/openapi.yaml 生成
- MySQL 8（InnoDB，utf8mb4）；sqlc + go-sql-driver/mysql；迁移 goose
- Asynq + Redis：队列、定时任务、重试；Redis 同时用于验证码与限频
- 登录：手机号 + 短信验证码（阿里云短信）；golang-jwt 短效访问令牌，刷新令牌按设备存 MySQL
- 文件：阿里云 OSS 私有桶，客户端用预签名地址直传
- AI：阿里云百炼（OpenAI 兼容接口，华北 2）为主，另一家国内平台对照；只经 internal/ai 调用
- 其他云服务：文字识别 OCR、智能语音交互、内容安全；支付：微信支付、支付宝、App Store 内购验证
- 日志 log/slog（JSON）；测试 testing + testcontainers-go；检查 gofmt、go vet、golangci-lint

## 目录

- cmd/api、cmd/worker、cmd/eval：三个入口，共用 internal
- api/openapi.yaml：接口契约；internal/gen/：生成代码，禁止手改
- internal/http/：路由、处理器、中间件（鉴权、幂等、审计、错误、功能开关）
- internal 下按业务域分包：auth、profile、bank、material、importer、practice、grading、paper、recite、essay、plan、score、quota、membership、payment、official、admin、notify、export
- internal/rules/：业务规则纯函数（PRD v3 第 11 节），参数读 rule_params
- internal/ai/：按能力封装模型调用、提示词（带版本号）、输出校验、灰度路由
- internal/cloud/：短信、OSS、OCR、语音、内容安全、支付的封装；本地默认 mock 实现
- internal/jobs/：Asynq 任务定义、处理器与定时任务
- db/migrations/：goose SQL 迁移；db/queries/：sqlc 查询；sqlc.yaml
- evals/：AI 评测数据集（JSONL）与门槛配置；真实样本放 evals/private/，不提交到仓库
- deploy/：docker-compose.app.yml、docker-compose.db.yml、nginx 配置、备份脚本

## 常用命令

- make dev：docker compose 起本地 MySQL 与 Redis，再启动 API 与 Worker
- make gen：按 openapi.yaml 与 db/queries 重新生成代码（改契约或 SQL 后必须执行）
- make migrate-new name=xxx / make migrate-up
- make test / make lint
- make eval cap=import|grading|essay|kp|ocr：跑某项 AI 能力的评测，输出指标与是否达标
- make api-tag v=api-v0.3：给契约打版本标签，供前端拉取

## 必须遵守

1. 一次只做一张任务卡；先输出实现计划等确认；不改卡片范围外的代码
2. 契约先行：改接口先改 api/openapi.yaml，再 make gen；破坏性变更要升版本或兼容旧 App
3. 改库只写新的迁移文件，已合并的迁移不修改；字段只加不删，删除分两次发布
4. 用户数据隔离：所有用户内容表带 owner_user_id；每条 sqlc 查询都带归属条件；每个用户内容接口都有「拿别人的 ID 访问返回 404」的集成测试
5. 后台看不到用户内容：admin 包不提供读取用户资料、题目、作答原文的查询；只有 content_access_grants 有效期内的授权查看接口能读，每次读写 content_access_logs 并给用户发消息
6. 业务规则只写在 internal/rules，参数从 rule_params 读取；注释写 PRD v3 节号（如 // PRD v3 11.6）；单元测试覆盖 PRD 里的例子与边界
7. 掌握度、预估分、额度、会员、兑换码只由服务端写；扣额度与写结果放在同一个事务里；批改失败、复核、改采分点重批不扣次数
8. AI 输出一律按 JSON Schema 校验；批改再校验分值上限、总分、引用必须是考生原话；知识点原文必须能在资料文本里逐字找到；改 internal/ai 或提示词后必须跑 make eval，低于门槛不得合并
9. 功能开关：在线支付、官方题库、口述背诵、扫描版 PDF、邀请默认关闭；开关支持「全部 / 指定用户」；关闭时相关接口返回 404 而不是报错
10. 密钥只从环境变量读取，不写入仓库、文档、日志和提交信息；不在开发会话里使用生产密钥或生产数据
11. 日志不记录手机号明文、作答原文、资料原文；对外文案不承诺分数
12. 写接口支持 idempotency_key；模拟考试以服务端 deadline_at 为准；Asynq 任务必须可重复执行，入队在事务提交之后，任务状态同时记在 MySQL
13. 数据库时间存 UTC（DATETIME(3)）；业务日期（每日计划、额度重置）按 Asia/Shanghai
14. 新增依赖要在提交说明里写明理由，只用主流、活跃维护的库

## 约定

- 路由前缀 /api/v1，后台接口 /api/v1/admin
- 错误响应统一为 { code, message, detail }，错误码集中定义在 internal/http/errors.go
- 标识符英文；注释写「为什么」；面向用户的文案用简体中文
- 分支 card/T07-profile；提交信息：T07: 备考档案与配置
- 本地默认 AI_PROVIDER=mock、SMS_PROVIDER=mock、OCR_PROVIDER=mock、PAY_PROVIDER=mock

## 环境变量（值不进仓库，示例见 .env.example）

APP_ENV、HTTP_ADDR、MYSQL_DSN、REDIS_ADDR、REDIS_PASSWORD、JWT_SECRET、
OSS_ENDPOINT、OSS_BUCKET、OSS_ACCESS_KEY_ID、OSS_ACCESS_KEY_SECRET、
SMS_PROVIDER、SMS_SIGN_NAME、SMS_TEMPLATE_CODE、
AI_PROVIDER、BAILIAN_BASE_URL、BAILIAN_API_KEY、AI_ALT_BASE_URL、AI_ALT_API_KEY、
OCR_PROVIDER、ASR_PROVIDER、MODERATION_PROVIDER、ALIYUN_ACCESS_KEY_ID、ALIYUN_ACCESS_KEY_SECRET、
PAY_PROVIDER、WECHAT_PAY_*、ALIPAY_*、APPLE_IAP_*

## 部署

- 只有一个生产环境：https://training.dreamelab.cn，API 在 /api/v1/，管理后台在 /admin/
- 应用机（4 核 16G）：nginx、api、worker、asynqmon（仅内网）；数据库机：mysql、redis，只对应用机开放内网端口
- 云效流水线：测试 → 构建镜像 → 推送 ACR → 备份数据库 → 执行迁移 → docker compose pull 与 up；回滚把镜像版本改回上一个
- 没有测试环境：新功能先用功能开关只对自己的账号打开

## 完成一张卡前自检

- [ ] make lint 与 make test 通过；make gen 后没有未提交的差异
- [ ] 逐条对照卡片「验收」写出结果
- [ ] 涉及的文档已更新（runbook、adr、open-questions、docs/tasks/README.md 的卡片状态）
- [ ] 列出需要在真机或生产环境手动验证的步骤

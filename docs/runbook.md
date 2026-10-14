# 运维手册（runbook）

生产只有一套：https://training.dreamelab.cn（API /api/v1/，后台 /admin/）。本项目复用现有 ECS 上的 `dreamer-mysql`、`dreamer-redis`、`dreamer-nginx`，应用容器加入同一个 `dreamer-net`。
本手册按顺序列出需要在阿里云控制台、现有 ECS 和云效上手动完成的步骤，每一步都写了怎么验证。
密钥只在控制台和服务器上填写，不写进仓库、文档和聊天。

部署文件都在 `deploy/`：

| 文件 | 用途 |
| --- | --- |
| docker-compose.app.yml | 接入现有 `dreamer-net` 的 api、worker、asynqmon |
| .env.app.example | 可提交仓库的应用服务环境变量模板（只有占位值，不含真实密钥） |
| db/init.sql | 本地或新建项目库的完整手动初始化 SQL |
| scripts/release.sh | 发布：备份 → 拉镜像 → 迁移 → 重启 → 健康检查，失败自动回滚 |
| scripts/rollback.sh | 回滚到上一版本或指定版本 |
| scripts/backup-mysql.sh | 全量备份并上传 OSS（每日 + 每次发布前） |
| scripts/backup-binlog.sh | 现有 MySQL 容器的 binlog 持续上传 OSS（按需启用） |
| scripts/restore-mysql.sh | 用备份恢复到指定库（演练用） |
| ci/test.sh、ci/build-push.sh、ci/deploy.sh | 云效流水线三个阶段调用的脚本 |

---

## 一、首次搭建（G1 之前，按顺序做）

### 1. 账号与权限

1. 阿里云主账号下建 RAM 子账号 `training-app`，只授予：OSS 业务桶读写、短信发送、文字识别、智能语音交互、内容安全、百炼调用。生成 AccessKey，**只填进 ECS .env**。
2. 再建 RAM 子账号 `training-backup`，只授予备份桶的写权限，AccessKey 只配置在 ECS 的 ossutil 里。

验证：RAM 控制台里两个子账号的权限策略只有上面这些。

### 2. 网络、OSS、镜像仓库

1. 专有网络 VPC（华北 2）里建一个交换机。
2. 现有 ECS 保留公网 IP（或 EIP），MySQL、Redis、Nginx 和本项目应用容器都运行在这台机器上，并加入 `dreamer-net`。
3. 安全组：
   - 应用机：入方向只开 22（限定你的办公 IP）、80、443
   - MySQL、Redis 不需要额外开放公网端口；3306、6379 只在 Docker 网络内使用
4. OSS：
   - 业务私有桶（如 `training-prod`），读写权限「私有」，开服务端加密（SSE-OSS）；跨域规则允许 App 直传（PUT，来源 `*`，允许头 `*`）
   - 备份桶（如 `training-backup`），私有，开服务端加密；生命周期规则：`mysql/` 前缀 30 天后删除
5. 容器镜像服务 ACR 个人版或企业版：建命名空间；推送本项目 API 镜像，并按需同步 `hibiken/asynqmon` 基础镜像。现有 MySQL、Redis、Nginx 镜像由你的 Compose 管理，不由本项目重复部署。
6. 给 ECS 数据盘设置自动快照策略（每日，保留 7 天）。

验证：从你的电脑访问 ECS 公网 `3306`、`6379` 应失败；在 ECS 上通过容器网络访问现有 MySQL、Redis 成功。

### 3. 现有 ECS 上准备项目数据库

先确认现有容器和网络，不要再执行本仓库的数据库 Compose：

```bash
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
docker network inspect dreamer-net
```

在现有 MySQL 容器中创建 `training`、`training_dev` 数据库和各自独立的应用账号。`training_dev` 供本机开发使用；生产 `training` 留给云效发布时的 Goose 迁移初始化。把 [db/init.sql](/Users/a1/Documents/workspace/myself/peetraining-server/db/init.sql) 手动执行到 `training_dev`，不要把它执行到现有 `dreamer_ai` 库。

```bash
docker exec -it <现有-mysql-容器名> mysql -uroot -p
CREATE DATABASE IF NOT EXISTS training CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE IF NOT EXISTS training_dev CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER IF NOT EXISTS 'training'@'%' IDENTIFIED BY '<生产应用随机密码>';
CREATE USER IF NOT EXISTS 'training_dev'@'%' IDENTIFIED BY '<本地开发随机密码>';
GRANT ALL PRIVILEGES ON training.* TO 'training'@'%';
GRANT ALL PRIVILEGES ON training_dev.* TO 'training_dev'@'%';
FLUSH PRIVILEGES;
```

手动执行并验证版本：

```bash
mysql -h 127.0.0.1 -P 3306 -uroot -p < db/init.sql
mysql -h 127.0.0.1 -P 3306 -u training_dev -p training_dev -e 'SELECT MAX(version_id) FROM goose_db_version;'
```

结果应为 `18`。生产库不要执行这份本地初始化文件；首次发布时流水线会通过 `/app/api migrate up` 按顺序创建表与默认配置。

建备份账号（应用账号 training 没有备份所需权限）：

```bash
docker exec -it <现有-mysql-容器名> mysql -uroot -p -e "
CREATE USER 'backup'@'%' IDENTIFIED BY '<随机密码>';
GRANT SELECT, RELOAD, LOCK TABLES, SHOW VIEW, TRIGGER, PROCESS, REPLICATION CLIENT, EVENT ON *.* TO 'backup'@'%';"
```

binlog 上传（安装并配置 ossutil，使用 training-backup 子账号）：

```bash
crontab -e
*/10 * * * * BACKUP_OSS_BUCKET=training-backup APP_DIR=/opt/training /opt/training/scripts/backup-binlog.sh >> /var/log/binlog-backup.log 2>&1
```

验证：
- `docker ps` 中现有 MySQL、Redis、Nginx 都是 running
- `docker network inspect dreamer-net` 能看到现有容器
- 你的电脑访问 ECS 公网 3306、6379 失败

### 4. 应用机

```bash
mkdir -p /opt/training/scripts /data/backup
cd /opt/training
# 安装 Docker、ossutil，并用 training-backup 子账号配置 ossutil
cp <仓库>/deploy/.env.app.example .env
chmod 600 .env
# ACR 若为私有仓库，在应用机预先登录，之后云效部署任务不必传 ACR 密码
docker login --username <ACR 用户名> registry.cn-beijing.aliyuncs.com
```

`deploy/.env.app.example` 是可以提交仓库的变量模板，真实 `/opt/training/.env` 不应提交。仓库已跟踪模板文件；不要为了提交真实配置而取消 `.gitignore` 对本地 `.env` 的忽略。先把 `CHANGE_ME` 全部替换掉，再核对：

| 配置 | 你需要填写什么 | 是否必填 |
| --- | --- | --- |
| `IMAGE` | ACR 完整镜像地址，含命名空间和 `peetraining-server` | 必填；必须与流水线 `ACR_REGISTRY/ACR_NAMESPACE` 拼出的地址一致 |
| `MYSQL_DSN` | training 应用账号、密码、MySQL 容器名和 `training` 库名 | 必填；账号应只授权 `training.*` |
| `REDIS_ADDR` / `REDIS_PASSWORD` / `REDIS_DB` | Redis 容器名、密码及未被其他业务占用的库号 | 必填；无密码时密码留空，模板使用 DB 1 |
| `JWT_SECRET` | 在安全终端运行 `openssl rand -hex 32` 生成 | 必填；不要重复使用本地开发值 |
| `BACKUP_MYSQL_DSN` | 单独的 backup 账号连接串 | 发布备份必填 |
| `BACKUP_OSS_BUCKET` | 已创建的私有备份桶名 | 发布备份必填；ECS 的 ossutil 要有该桶写权限 |
| `BAILIAN_API_KEY` | 百炼 API Key | 使用 AI 时必填；模板默认选择已接入的百炼客户端 |
| `SMS_*` | 短信签名与模板 | 当前无法通过配置启用，代码还没有生产短信实现，见下方阻塞说明 |
| `OSS_*`、`OCR_*`、`ASR_*`、`MODERATION_*` | 真实云服务凭据 | 当前代码尚未接入生产 provider，不能仅靠填 Key 启用 |
| `WECHAT_PAY_*`、`ALIPAY_*`、`APPLE_IAP_*` | 已开通支付渠道的完整凭据 | 只在开放对应支付渠道时填写，并将 `PAY_PROVIDER=real` |

首次部署模板的 `VERSION=bootstrap` 只是基准标记；部署脚本发布时会替换为本次镜像 tag。首次部署没有已运行的旧版本，健康检查失败时会停止新 API/Worker 并报错，不会尝试回滚到不存在的旧镜像。后续发布才有自动回滚目标。

应用实际连接的是 Docker 网络内的容器名，不是 ECS 公网 IP。执行 `docker ps` 确认 MySQL、Redis 容器名；再用 `docker network inspect dreamer-net` 确认这些容器和本项目都在同一个网络。数据库密码建议使用 `openssl rand -hex 24`，避免 DSN 分隔符影响解析。

**当前不能直接正式上线。** `internal/cloud` 目前只有 mock 短信，production 明确拒绝 mock，而 `SMS_PROVIDER=aliyun` 也尚未实现；因此 API 和 Worker 会启动失败。OSS、OCR、ASR、内容安全也只有 mock provider。先完成这些生产 provider 接入和真实凭据验证，再执行首次发布。`/app/api migrate up` 单独可能成功，但它通过不代表 API 服务能启动。

现有 `dreamer-nginx` 负责 80/443、证书和前端静态文件。它需要加入 `dreamer-net`，并把 `/api/` 反向代理到 `http://training-api:8080`；证书、域名和 `/admin/` 仍按你现有 Nginx 配置管理。

每日备份：

```bash
crontab -e
30 18 * * * APP_DIR=/opt/training /opt/training/scripts/backup-mysql.sh daily >> /var/log/mysql-backup.log 2>&1
```
（UTC 18:30 = 北京时间 02:30）

验证：`dig training.dreamelab.cn` 解析到应用机；证书文件存在且权限 600。

### 5. 云效流水线

1. 云效「代码管理」仓库选 `peetraining-server`，代码源分支先选 `main`。首次上线先用手动触发；验收通过后再打开 main 提交自动触发。
2. 添加流水线变量：构建阶段必填 `ACR_REGISTRY`（如 `registry.cn-beijing.aliyuncs.com`）、`ACR_NAMESPACE`、`ACR_USERNAME`、`ACR_PASSWORD`；用户名和密码勾选加密/保密。`GO_IMAGE`、`RUNTIME_IMAGE` 是可选项，只在基础镜像已同步到 ACR 时设置。`VERSION` 可不设置，脚本默认使用 Git tag 或提交号。
3. 按截图中的节点配置阶段：
   - **测试**：添加「执行命令」任务，使用带 Docker 的 Linux 构建机，执行 `bash deploy/ci/test.sh`。
   - **Go 构建 Docker 镜像并推送镜像仓库**：添加「执行命令」任务，执行 `bash deploy/ci/build-push.sh`。成功后脚本在制品目录写入 `.release-version`。
   - **制品传递**：把代码目录中的 `deploy/` 与 `.release-version` 作为流水线制品传给部署阶段；不要把服务器 `.env`、`.env.local` 或任何密钥打包。
   - **Docker 部署**：主机组选应用 ECS，执行用户选可运行 Docker 的部署账号（截图里使用 root 也可），部署脚本填写：
     ```bash
     set -euo pipefail
     source .release-version
     bash deploy/ci/deploy.sh "$VERSION"
     ```
4. 私有 ACR 镜像拉取：要么在 ECS 上预先执行一次 `docker login`，要么给「Docker 部署」任务配置加密变量 `ACR_REGISTRY`、`ACR_USERNAME`、`ACR_PASSWORD`，脚本会在主机上登录。云效构建机的登录态不会自动传到 ECS。
5. 应用 ECS 先安装云效主机部署 Agent，并按上面的模板单独建立 `/opt/training/.env`，权限设为 `600`。部署脚本会保留这个文件，并检查 `dreamer-net` 网络存在。

验证：provider 实现完成后，手动运行流水线；三个阶段均成功，`https://training.dreamelab.cn/api/v1/health` 返回 200，`version` 与 `.release-version` 一致。首次成功后再启用 main 自动触发。

### 6. G1 验收清单

- [ ] `curl https://training.dreamelab.cn/api/v1/health` 返回 200，`mysql`、`redis` 都是 `ok`
- [ ] 浏览器打开 https://training.dreamelab.cn/admin/ 能看到页面
- [ ] 从公网连 ECS 的 3306、6379 失败；应用容器能通过 dreamer-net 访问
- [ ] 手动执行 `/opt/training/scripts/backup-mysql.sh manual`，OSS 备份桶 `mysql/full/` 下有文件，并按「三、备份恢复」在本地恢复成功
- [ ] 改一行代码提交，流水线自动发布成功；再执行一次 `/opt/training/scripts/rollback.sh`，健康检查通过且版本号回到上一个

---

### 7. 第一个后台管理员（T28）

后台账号不能在页面上自助注册。第一个管理员在应用机上用命令行建，初始密码从环境变量读（不出现在命令行历史里），首次登录必须修改：

```
read -s ADMIN_INITIAL_PASSWORD && export ADMIN_INITIAL_PASSWORD
docker compose -f docker-compose.app.yml run --rm -e ADMIN_INITIAL_PASSWORD api create-admin <账号> <显示名> <手机号> admin
unset ADMIN_INITIAL_PASSWORD
```

登录时短信验证码发到这个手机号（两步验证强制开启）。其他账号之后在 7.15 管理（T29）。

### 8. AI 模型与价格（T31）

1. 按 ADR 0012 用真实样本评测选定模型后，在应用机 .env 填 AI_PROVIDER=bailian、BAILIAN_BASE_URL、BAILIAN_API_KEY、AI_MODEL_STRONG、AI_MODEL_CHEAP、AI_PREFER_STRONG，
   对照平台填 AI_ALT_BASE_URL、AI_ALT_API_KEY。生产不用 AI_PROVIDER=relay（中转接口不是备案模型，D20）。
2. AI_PRICES 按平台当时的公开价填写（元 / 百万 token，「模型=输入价:输出价」逗号分隔），覆盖上面用到的每个模型；不填时 7.8 成本看板与预算告警都是 0。
   平台调价后同步改这里。

验证：用测试账号批改一道主观题，后台 7.8 AI 任务里 grade_subjective 的单次成本不为 0。

### 9. 监控告警与埋点（T32）

1. SLS 采集应用机 `/var/lib/docker/containers/*/*-json.log`（api、worker 两个容器），开启 JSON 解析，对字段 `log_type`、`level`、`event`、`alert`、`route`、`status` 建索引。
2. 告警：Worker 每 5 分钟检查一次，超过阈值写一条 `log_type=alert` 的 ERROR 日志；配了 `ALERT_WEBHOOK_URL`（钉钉或企业微信群机器人，含令牌，只放 .env）时同时推送。同一项 30 分钟内只发一次。
   | 告警 | 阈值 | 先做什么 |
   | --- | --- | --- |
   | http_errors | 近 15 分钟 5xx 占比 > 2%（至少 50 个请求） | 按 route 聚合 status ≥ 500 的日志，看是哪个接口；必要时回滚 |
   | queue_backlog:<队列> | 等待 > 200 个，或最老任务等了 > 10 分钟 | worker 是否在跑（`docker compose ps`）；asynqmon 看失败最多的任务 |
   | ai_failures | 近 15 分钟 AI 调用失败率 > 10%（至少 20 次） | 后台 7.8 看哪个能力在失败；模型平台故障时会自动切备用，提示词问题回滚灰度 |
   | ai_cost_user / ai_cost_revenue | 每活跃用户每天成本超预算；本月成本占收入超上限（7.8 可改） | 下调免费额度或暂时关闭高成本功能 |
   另在 SLS 上配一条兜底告警：5 分钟内没有任何 `log_type=alert` 以外的 worker 日志（worker 挂了时它自己发不出告警）。
3. 埋点：App 调 POST /events，服务端写 `log_type=event` 的日志（只有事件名、公共字段与短的标量属性，没有作答和资料内容）。在 SLS 里按 event 做漏斗和留存（PRD 15 节的内测指标）；后台 7.1 概览的漏斗来自数据库统计，两边可以互相核对。

验证：临时把 worker 停 15 分钟，群里收到 queue_backlog 告警；App 里做一次练习，SLS 搜 `log_type: event and event: answer_submit` 有记录。

### 10. 内测准备（T32）

1. 协议：docs/legal/ 里的草稿经法务审核定稿后，在后台 7.8「协议」新建 user、privacy、membership 1.0 并发布；用新账号登录看 0.4 正文。
2. 种子用户兑换码：后台 7.4 新建批次（名称写「内测种子」、天数、数量 ≤ 50、有效期），导出 CSV 线下发给种子用户；7.4 可看每个码的使用情况。
3. 功能开关：内测期打开哪些功能（在线支付、官方题库、口述背诵、语音作答、扫描版 PDF、邀请）按当时决定在 7.8 设置。
4. 越权访问：`make test` 里的 TestIntegrationIDOR 会用另一个用户的 ID 调全部 App 接口，必须全部 404；新增接口自动纳入。
5. 发版前跑一遍 `make eval-all`（真实样本），都达标才发。

## 二、日常发布与回滚

- 发布：合并到 main 自动触发流水线。发布脚本先备份数据库，迁移失败时不会切换版本；健康检查 60 秒内不通过会自动回滚。
- 手动发布指定版本：应用机上 `/opt/training/scripts/release.sh v0.1.3`
- 回滚：`/opt/training/scripts/rollback.sh`（回到上一版本）或 `rollback.sh v0.1.1`。迁移只加不删，回滚镜像不需要回滚数据库。
- 新功能先用功能开关只对自己的账号打开（后台 7.8），在生产上自测后再对全部用户打开。
- 查看队列：`ssh -L 8081:127.0.0.1:8081 应用机`，浏览器打开 http://localhost:8081（asynqmon 只绑定在本机回环地址）。

## 三、备份恢复

备份：每日全量（`mysql/full/`）+ 每次发布前全量 + 每 10 分钟上传 binlog（`mysql/binlog/`），OSS 保留 30 天；云盘每日快照保留 7 天。

**每月演练一次**（恢复到本地或临时库，不要对生产库执行）：

```bash
ossutil cp oss://training-backup/mysql/full/<最近的文件>.sql.gz .
docker run -d --name restore-test -p 127.0.0.1:3307:3306 -e MYSQL_ROOT_PASSWORD=test mysql:8.4
MYSQL_PWD=test deploy/scripts/restore-mysql.sh <文件>.sql.gz 127.0.0.1 3307 root training
# 核对用户数、最近的迁移版本
docker exec restore-test mysql -uroot -ptest training -e 'SELECT COUNT(*) FROM users; SELECT MAX(version_id) FROM goose_db_version;'
```

按时间点恢复（误删数据时）：先恢复最近一次全量；备份文件开头的 `-- CHANGE REPLICATION SOURCE TO SOURCE_LOG_FILE=..., SOURCE_LOG_POS=...` 记录了 binlog 位置；
下载之后的 binlog，用 `mysqlbinlog --start-position=<位置> --stop-datetime='<误操作之前的时间>' mysql-bin.0000xx ... | mysql ...` 回放。

演练结果记在本节末尾：

| 日期 | 备份文件 | 恢复耗时 | 用户数核对 | 执行人 |
| --- | --- | --- | --- | --- |

## 四、故障排查

| 现象 | 先看 | 处理 |
| --- | --- | --- |
| 健康检查 503，mysql=error | ECS 上现有 MySQL 容器日志、磁盘 `df -h` | 检查 MySQL 容器与 dreamer-net；磁盘满时清理 binlog（确认已上传 OSS） |
| 健康检查 503，redis=error | ECS 上现有 Redis 容器日志 | 检查 Redis 容器与 dreamer-net；AOF 损坏时使用 Redis 工具修复 |
| 502 Bad Gateway | `docker compose -f docker-compose.app.yml ps`、`logs api` | api 启动失败多为 .env 缺项（日志会写明哪个变量）；修正后 `up -d api` |
| 发布时迁移失败 | 流水线日志里的迁移报错 | 版本未切换、服务不受影响；修迁移后重新发布（不要改已合并的迁移文件，写新的） |
| 队列积压 | asynqmon 各队列 pending 数 | 看 worker 日志里失败最多的任务类型；AI 服务异常时在后台 7.8 回滚提示词版本或模型 |
| AI 成本异常 | 后台 7.8 成本统计 | 下调免费额度（7.8）或暂时关闭对应功能开关 |
| 用户付了钱没开通会员 | api 日志搜订单号：「支付回调验签失败」「支付回调开通失败」「订单重复支付」；订单表 status | 验签失败多为平台公钥或 APIv3 密钥配错；金额不符、重复支付需人工核对后在后台 7.3 处理（ADR 0011）；渠道会按自身策略重发回调，修好配置后通常自动补开通 |
| 证书过期 | 浏览器提示 | 续期并替换 certs 下的文件，`nginx -s reload` |

日志：容器日志 `docker compose logs --since 1h api`；SLS 采集应用机 `/var/lib/docker/containers/*/*-json.log`（T32 配置告警）。
日志里没有手机号明文、作答原文和资料原文，排查用户问题用请求 ID（响应头 X-Request-ID）定位。

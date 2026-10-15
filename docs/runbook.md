# 运维手册（runbook）

生产只有一套：https://training.dreamelab.cn（API /api/v1/，后台 /admin/）。本项目复用现有 ECS 上的 `dreamer-mysql`、`dreamer-redis`、`dreamer-nginx`，应用容器加入同一个 `dreamer-net`。
本手册按顺序列出需要在阿里云控制台、现有 ECS 和云效上手动完成的步骤，每一步都写了怎么验证。
密钥只在控制台和服务器上填写，不写进仓库、文档和聊天。

部署文件都在 `deploy/`：

| 文件 | 用途 |
| --- | --- |
| docker-compose.yml | 接入现有 `dreamer-net` 的 api、worker、asynqmon；每次启动自动拉取 latest |
| .env.app.example | 可提交仓库的应用服务环境变量模板（只有占位值，不含真实密钥） |
| db/init.sql | 本地或新建项目库的完整手动初始化 SQL |
| ci/test.sh、ci/build-push.sh | 可选 CI 脚本；当前流水线使用云效原生 Go 与 ACR 构建节点时不需要它们 |

---

## 一、首次搭建（G1 之前，按顺序做）

### 1. 账号与权限

1. 阿里云主账号下建 RAM 子账号 `training-app`，只授予：OSS 业务桶读写、短信发送、文字识别、智能语音交互、内容安全、百炼调用。生成 AccessKey，**只填进 ECS .env**。

验证：RAM 控制台里子账号的权限策略只有上面这些。

### 2. 网络、OSS、镜像仓库

1. 专有网络 VPC（华北 2）里建一个交换机。
2. 现有 ECS 保留公网 IP（或 EIP），MySQL、Redis、Nginx 和本项目应用容器都运行在这台机器上，并加入 `dreamer-net`。
3. 安全组：
   - 应用机：入方向只开 22（限定你的办公 IP）、80、443
   - MySQL、Redis 不需要额外开放公网端口；3306、6379 只在 Docker 网络内使用
4. OSS：
   - 业务私有桶（如 `training-prod`），读写权限「私有」，开服务端加密（SSE-OSS）；跨域规则允许 App 直传（PUT，来源 `*`，允许头 `*`）
5. 容器镜像服务 ACR 个人版或企业版：建命名空间；推送本项目 API 镜像，并按需同步 `hibiken/asynqmon` 基础镜像。现有 MySQL、Redis、Nginx 镜像由你的 Compose 管理，不由本项目重复部署。

验证：从你的电脑访问 ECS 公网 `3306`、`6379` 应失败；在 ECS 上通过容器网络访问现有 MySQL、Redis 成功。

### 3. 现有 ECS 上准备项目数据库

先确认现有容器和网络，不要再执行本仓库的数据库 Compose：

```bash
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
docker network inspect dreamer-net
```

在现有 MySQL 容器中创建 `training`、`training_dev` 数据库和各自独立的应用账号。`training_dev` 供本机开发使用；生产 `training` 按已完成的数据库初始化准备。把 [db/init.sql](/Users/a1/Documents/workspace/myself/peetraining-server/db/init.sql) 手动执行到 `training_dev`，不要把它执行到现有 `dreamer_ai` 库。

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

结果应为 `18`。生产库不要执行这份本地初始化文件；生产库表结构已提前初始化，发布流水线不会执行数据库迁移。以后有新的迁移时，先单独在数据库执行并确认完成，再发布对应服务版本。

验证：
- `docker ps` 中现有 MySQL、Redis、Nginx 都是 running
- `docker network inspect dreamer-net` 能看到现有容器
- 你的电脑访问 ECS 公网 3306、6379 失败

### 4. 应用机

```bash
mkdir -p /opt/dreamer/project/training
cp <仓库>/deploy/docker-compose.yml /opt/dreamer/project/training/docker-compose.yml
cp <仓库>/deploy/.env.app.example /opt/dreamer/project/training/.env
chmod 600 /opt/dreamer/project/training/.env
cd /opt/dreamer/project/training
docker compose version # 必须是 Docker Compose v2 插件
# 私有 ACR 只需在应用机登录一次，凭据由 Docker 保存在服务器上
docker login --username <ACR 用户名> crpi-3mjxc296umgnl4w9.cn-beijing.personal.cr.aliyuncs.com
```

`docker login`、`.env` 文件所有权和云效部署节点使用同一个 Linux 用户；该用户需要能访问 Docker daemon。

如果 `/opt/dreamer/project/training` 是空目录，这是首次部署，必须先把仓库中的 `deploy/docker-compose.yml` 复制到该目录，并复制 `.env.app.example` 为 `.env` 后填写生产值。`docker-compose.yml` 是配置文件名；Docker Compose v2 的命令写作 `docker compose`（中间有空格）。Compose 不会从已有容器推断项目目录或自动生成这两个文件。

`deploy/.env.app.example` 是可以提交仓库的变量模板，真实 `/opt/dreamer/project/training/.env` 不应提交。仓库已跟踪模板文件；不要为了提交真实配置而取消 `.gitignore` 对本地 `.env` 的忽略。先把 `CHANGE_ME` 全部替换掉，再核对：

如果这台 ECS 之前按旧版手册配置过 MySQL 备份，先在 `crontab -e` 删除调用 `backup-mysql.sh` 或 `backup-binlog.sh` 的行，再删除 `/opt/dreamer/project/training/scripts/` 下这两个旧脚本，并从 `/opt/dreamer/project/training/.env` 删除不再使用的 `BACKUP_*` 项。此调整不删除 OSS 中已有文件，也不删除 MySQL 数据。

| 配置 | 你需要填写什么 | 是否必填 |
| --- | --- | --- |
| `IMAGE` | ACR 完整镜像地址，含命名空间和 `peetraining-server` | 必填；必须与流水线 `ACR_REGISTRY/ACR_NAMESPACE` 拼出的地址一致 |
| `MYSQL_DSN` | training 应用账号、密码、MySQL 容器名和 `training` 库名 | 必填；账号应只授权 `training.*` |
| `REDIS_ADDR` / `REDIS_PASSWORD` / `REDIS_DB` | Redis 容器名、密码及未被其他业务占用的库号 | 必填；无密码时密码留空，模板使用 DB 1 |
| `JWT_SECRET` | 在安全终端运行 `openssl rand -hex 32` 生成 | 必填；不要重复使用本地开发值 |
| `BAILIAN_API_KEY` | 百炼 API Key | 使用 AI 时必填；模板默认选择已接入的百炼客户端 |
| `SMS_*` | 短信签名与模板 | 当前无法通过配置启用，代码还没有生产短信实现，见下方阻塞说明 |
| `OSS_*`、`OCR_*`、`ASR_*`、`MODERATION_*` | 真实云服务凭据 | 当前代码尚未接入生产 provider，不能仅靠填 Key 启用 |
| `WECHAT_PAY_*`、`ALIPAY_*`、`APPLE_IAP_*` | 已开通支付渠道的完整凭据 | 只在开放对应支付渠道时填写，并将 `PAY_PROVIDER=real` |

镜像统一使用 ACR 的 `latest` tag，不需要配置 `VERSION`。Compose 每次启动都会拉取最新镜像；API 健康探测由镜像内的 `/app/api healthcheck` 执行，可用 `docker compose ps` 查看状态。

应用实际连接的是 Docker 网络内的容器名，不是 ECS 公网 IP。执行 `docker ps` 确认 MySQL、Redis 容器名；再用 `docker network inspect dreamer-net` 确认这些容器和本项目都在同一个网络。数据库密码建议使用 `openssl rand -hex 24`，避免 DSN 分隔符影响解析。

**当前不能直接正式上线。** `internal/cloud` 目前只有 mock 短信，production 明确拒绝 mock，而 `SMS_PROVIDER=aliyun` 也尚未实现；因此 API 和 Worker 会启动失败。OSS、OCR、ASR、内容安全也只有 mock provider。先完成这些生产 provider 接入和真实凭据验证，再执行首次发布。数据库迁移需要在发布前单独完成，Compose 不会自动执行迁移。

现有 `dreamer-nginx` 负责 80/443、证书和前端静态文件。它需要加入 `dreamer-net`，并把 `/api/` 反向代理到 `http://training-api:8080`；证书、域名和 `/admin/` 仍按你现有 Nginx 配置管理。

验证：`dig training.dreamelab.cn` 解析到应用机；证书文件存在且权限 600。

### 5. 云效流水线

1. 云效「代码管理」仓库选 `peetraining-server`，代码源分支先选 `main`。首次上线先用手动触发；验收通过后再打开 main 提交自动触发。
2. 不需要定义 `VERSION` 流水线变量。镜像固定推送到 `latest`，应用机的 Compose 也固定拉取 `latest`。原生镜像任务使用 ACR 服务连接；部署私有镜像时，还需让 ECS 能拉取 ACR 镜像。
   - `FONT_IMAGE` 是 PDF 导出字体资源；`RUNTIME_IMAGE` 是 API/Worker 最终运行环境。基础镜像已导入到你的 ACR：
     ```bash
     ACR_REGISTRY=crpi-3mjxc296umgnl4w9.cn-beijing.personal.cr.aliyuncs.com
     ACR_NAMESPACE=dreamer-lab
     ```
     本地不安装 Docker 时，可用 Skopeo 在镜像仓库之间直接复制。登录时输入 ACR 个人实例的用户名和访问凭证，不要使用 `sudo`。本机无法直连 Docker Hub/GCR 时，下面使用已验证可访问的镜像代理；复制完成后可用 `skopeo inspect --raw docker://<ACR地址>/<命名空间>/<仓库>:<tag>` 检查 manifest。
     ```bash
     brew install skopeo
     skopeo login crpi-3mjxc296umgnl4w9.cn-beijing.personal.cr.aliyuncs.com
     skopeo copy --all docker://docker.1ms.run/library/debian:bookworm-slim \
       docker://crpi-3mjxc296umgnl4w9.cn-beijing.personal.cr.aliyuncs.com/dreamer-lab/debian:bookworm-slim
     skopeo copy --all docker://gcr.m.daocloud.io/distroless/static-debian12:nonroot \
       docker://crpi-3mjxc296umgnl4w9.cn-beijing.personal.cr.aliyuncs.com/dreamer-lab/distroless-static-debian12:nonroot
     ```
     两个 tag 的 ACR manifest digest 应分别与代理源一致；不要在每次发布任务里重新拉取公网镜像。
   - **字体镜像一次性构建**：基础 Debian 镜像导入后，在云效运行一次现有「镜像构建并推送至 ACR」任务，Dockerfile 路径设为 `deploy/docker/Dockerfile.fonts`，构建上下文为仓库根目录，镜像仓库设为 `crpi-3mjxc296umgnl4w9.cn-beijing.personal.cr.aliyuncs.com/dreamer-lab/peetraining-fonts`，tag 设为 `bookworm`，构建参数填：
     ```text
     --build-arg FONT_BASE_IMAGE=crpi-3mjxc296umgnl4w9.cn-beijing.personal.cr.aliyuncs.com/dreamer-lab/debian:bookworm-slim
     ```
     这一步只需在字体或基础 Debian 版本变更时重跑。完成后 ACR 中会有一个只包含 Droid Sans Fallback 与 DejaVu Sans 字体文件的小镜像，日常服务发布不会再执行 `apt-get`。
     云效镜像任务所用 ACR 服务连接需要对 `dreamer-lab` 命名空间有拉取基础镜像和推送镜像的权限。
3. 按流水线节点配置阶段：
   - **测试**：部署不是测试流水线，测试节点可以先不添加；需要时再单独运行 `bash deploy/ci/test.sh`。
   - **Go 构建**：选择云效前一步已经安装好的 Go 1.25.3 和 Linux/amd64；在「执行 Golang 构建命令」中选择 Bash，然后粘贴。该命令不安装 Go，也不设置版本号：
     ```bash
     set -euo pipefail
     export GOPROXY=https://goproxy.cn,direct
     export GOTOOLCHAIN=local
     go version
     case "$(go env GOVERSION)" in
       go1.25.*) ;;
       *) echo "构建机不是 Go 1.25：$(go env GOVERSION)" >&2
         exit 2
         ;;
     esac
     export GOOS=linux GOARCH=amd64 CGO_ENABLED=0
     make build
     test -s bin/api && test -s bin/worker
     ```
     `GOPROXY` 只负责模块下载代理；`GOTOOLCHAIN=local` 强制使用云效已安装的 Go，不会自动下载 1.27。项目 `go.mod` 已调整为 Go 1.25 兼容依赖。
   - **构建物上传**：上传 `bin/`、`Dockerfile` 和 `.dockerignore`，保留这些相对路径，供镜像构建节点下载。镜像任务在构建物根目录下应能看到 Dockerfile 与 `bin/api`、`bin/worker`。Compose 文件和生产 `.env` 已在应用机 `/opt/dreamer/project/training` 首次准备，日常部署不下载构建物；不要上传 `.env` 或其他密钥。
   - **镜像构建并推送**：保留你已有的云效原生任务，使用截图里的 ACR 服务连接；构建源选择上一步的构建物。镜像仓库填 `crpi-3mjxc296umgnl4w9.cn-beijing.personal.cr.aliyuncs.com/dreamer-lab/peetraining-server`，tag 固定填 `latest`。Dockerfile 路径填 `Dockerfile`，ContextPath 指向构建物根目录（其中应能直接看到 `bin/`）。「更多构建参数」填：
     ```text
     --build-arg FONT_IMAGE=crpi-3mjxc296umgnl4w9.cn-beijing.personal.cr.aliyuncs.com/dreamer-lab/peetraining-fonts:bookworm --build-arg RUNTIME_IMAGE=crpi-3mjxc296umgnl4w9.cn-beijing.personal.cr.aliyuncs.com/dreamer-lab/distroless-static-debian12:nonroot
     ```
     Go 二进制由 Go 步骤编译，Dockerfile 在此处直接复制进运行镜像；若 `bin/api` 或 `bin/worker` 不在构建上下文根目录，镜像构建会因 `COPY` 找不到文件而失败。
     如果日志在镜像构建开始前就报 `dockerAcrPersonal`、`GetAuthorizationToken`、`AUTHENTICATION_FAILED` 或 `user jurisdiction error`，这是云效 ACR 服务连接使用的阿里云身份没有获取 ACR 授权令牌的权限。核对该连接绑定的阿里云账号/RAM 用户是否就是拥有此 ACR 实例的账号；给连接实际使用的 RAM 用户授予 ACR 管理权限后，回到云效重新授权或保存服务连接，再重跑流水线。不要改 Dockerfile、Go 命令或镜像构建参数来处理这个错误。
   - **Docker 部署**：主机组选应用 ECS，执行用户选可运行 Docker 的部署账号；不下载构建物。确保服务器目录已按第 4 步放入 `docker-compose.yml` 和填写好的 `.env`。脚本步骤填写：
     ```bash
     set -eu
     cd /opt/dreamer/project/training || exit 1
     test -f docker-compose.yml || { echo "缺少 /opt/dreamer/project/training/docker-compose.yml，请先按首次部署步骤复制 Compose 文件" >&2; exit 1; }
     test -f .env || { echo "缺少 /opt/dreamer/project/training/.env，请先配置生产环境变量" >&2; exit 1; }
     docker compose version
     docker compose up -d
     docker compose ps
     ```
   - 私有 ACR 登录：在首次准备 ECS 时执行一次 `docker login`，之后 Compose 使用服务器保存的凭据拉取镜像。不要把 ACR 密码配置到每次部署任务中；云效构建节点的登录态不会自动传到 ECS。
   - `bash deploy/ci/build-push.sh` 是另一种完整构建并推送方案；选择它时才由脚本构建镜像，不要和云效原生镜像推送任务同时使用。
5. 应用 ECS 安装云效主机部署 Agent；`/opt/dreamer/project/training/docker-compose.yml`、权限为 `600` 的 `/opt/dreamer/project/training/.env` 和现有 `dreamer-net` 网络必须预先存在。云效部署节点在该目录执行 Compose 命令。

验证：provider 实现完成后，手动运行流水线；构建和部署都成功，`docker compose ps` 中 api 状态为 healthy，`https://training.dreamelab.cn/api/v1/health` 返回 200。Compose 会自动拉取 ACR 中的 `latest` 镜像。Asynqmon 随 Compose 启动，只绑定应用机回环地址。首次成功后再启用 main 自动触发。

### 6. G1 验收清单

- [ ] `curl https://training.dreamelab.cn/api/v1/health` 返回 200，`mysql`、`redis` 都是 `ok`
- [ ] 浏览器打开 https://training.dreamelab.cn/admin/ 能看到页面
- [ ] 从公网连 ECS 的 3306、6379 失败；应用容器能通过 dreamer-net 访问
- [ ] 改一行代码提交，流水线自动发布成功；Compose 将 API 标记为 healthy，异常时可查看容器日志排查

---

### 7. 第一个后台管理员（T28）

后台账号不能在页面上自助注册。第一个管理员在应用机上用命令行建，初始密码从环境变量读（不出现在命令行历史里），首次登录必须修改：

```
read -s ADMIN_INITIAL_PASSWORD && export ADMIN_INITIAL_PASSWORD
docker compose run --rm -e ADMIN_INITIAL_PASSWORD api create-admin <账号> <显示名> <手机号> admin
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

## 二、日常发布

- 发布：合并到 main 自动触发流水线。数据库迁移需在发布前单独完成；流水线把镜像推送为 `latest`，云效部署节点在 `/opt/dreamer/project/training` 执行 `docker compose up -d`，Compose 自动拉取并更新容器。
- 手动重新拉取并发布：应用机上执行 `cd /opt/dreamer/project/training && docker compose up -d`。
- 健康检查失败：查看 `docker compose ps` 和 `docker compose logs api worker`，修复配置或代码后重新运行流水线；Compose 不会执行数据库迁移。
- 新功能先用功能开关只对自己的账号打开（后台 7.8），在生产上自测后再对全部用户打开。
- 查看队列：`ssh -L 8081:127.0.0.1:8081 应用机`，浏览器打开 http://localhost:8081（asynqmon 只绑定在本机回环地址）。

## 三、故障排查

| 现象 | 先看 | 处理 |
| --- | --- | --- |
| 健康检查 503，mysql=error | ECS 上现有 MySQL 容器日志、磁盘 `df -h` | 检查 MySQL 容器与 dreamer-net、数据盘剩余空间；不要直接删除 MySQL 数据文件 |
| 健康检查 503，redis=error | ECS 上现有 Redis 容器日志 | 检查 Redis 容器与 dreamer-net；AOF 损坏时使用 Redis 工具修复 |
| 502 Bad Gateway | `docker compose ps`、`docker compose logs api` | api 启动失败多为 .env 缺项（日志会写明哪个变量）；修正后在 `/opt/dreamer/project/training` 执行 `docker compose up -d` |
| 数据库结构不匹配 | 应用日志里的 SQL/字段报错 | 先单独执行已审核的迁移 SQL，再重新发布（不要改已合并的迁移文件，写新的） |
| 队列积压 | asynqmon 各队列 pending 数 | 看 worker 日志里失败最多的任务类型；AI 服务异常时在后台 7.8 回滚提示词版本或模型 |
| AI 成本异常 | 后台 7.8 成本统计 | 下调免费额度（7.8）或暂时关闭对应功能开关 |
| 用户付了钱没开通会员 | api 日志搜订单号：「支付回调验签失败」「支付回调开通失败」「订单重复支付」；订单表 status | 验签失败多为平台公钥或 APIv3 密钥配错；金额不符、重复支付需人工核对后在后台 7.3 处理（ADR 0011）；渠道会按自身策略重发回调，修好配置后通常自动补开通 |
| 证书过期 | 浏览器提示 | 续期并替换 certs 下的文件，`nginx -s reload` |

日志：容器日志 `docker compose logs --since 1h api`；SLS 采集应用机 `/var/lib/docker/containers/*/*-json.log`（T32 配置告警）。
日志里没有手机号明文、作答原文和资料原文，排查用户问题用请求 ID（响应头 X-Request-ID）定位。

# 运维手册（runbook）

生产只有一套：https://training.dreamelab.cn（API /api/v1/，后台 /admin/）。全部资源在阿里云华北 2（北京）。
本手册按顺序列出需要在阿里云控制台、云效、两台服务器上手动完成的步骤，每一步都写了怎么验证。
密钥只在控制台和服务器上填写，不写进仓库、文档和聊天。

部署文件都在 `deploy/`：

| 文件 | 用途 |
| --- | --- |
| docker-compose.app.yml | 应用机：nginx、api、worker、asynqmon |
| docker-compose.db.yml | 数据库机：mysql、redis |
| nginx/training.conf | HTTPS、/api/ 反向代理、/admin/ 静态目录 |
| .env.app.example / .env.db.example | 两台机器 .env 的模板（只有占位值） |
| scripts/release.sh | 发布：备份 → 拉镜像 → 迁移 → 重启 → 健康检查，失败自动回滚 |
| scripts/rollback.sh | 回滚到上一版本或指定版本 |
| scripts/backup-mysql.sh | 全量备份并上传 OSS（每日 + 每次发布前） |
| scripts/backup-binlog.sh | binlog 持续上传 OSS（数据库机，每 10 分钟） |
| scripts/restore-mysql.sh | 用备份恢复到指定库（演练用） |
| ci/test.sh、ci/build-push.sh、ci/deploy.sh | 云效流水线三个阶段调用的脚本 |

---

## 一、首次搭建（G1 之前，按顺序做）

### 1. 账号与权限

1. 阿里云主账号下建 RAM 子账号 `training-app`，只授予：OSS 业务桶读写、短信发送、文字识别、智能语音交互、内容安全、百炼调用。生成 AccessKey，**只填进应用机 .env**。
2. 再建 RAM 子账号 `training-backup`，只授予备份桶的写权限，AccessKey 只配置在两台机器的 ossutil 里。

验证：RAM 控制台里两个子账号的权限策略只有上面这些。

### 2. 网络、OSS、镜像仓库

1. 专有网络 VPC（华北 2）里建一个交换机。
2. 两台 ECS 放在同一个 VPC：
   - 应用机 4 核 16G，分配公网 IP（或 EIP）
   - 数据库机 2 核 4G 以上（Q09），**不分配公网 IP**，另挂一块数据盘（开云盘加密）
3. 安全组：
   - 应用机：入方向只开 22（限定你的办公 IP）、80、443
   - 数据库机：入方向只允许应用机内网 IP 访问 3306、6379，22 只允许应用机内网 IP（从应用机跳转登录）
4. OSS：
   - 业务私有桶（如 `training-prod`），读写权限「私有」，开服务端加密（SSE-OSS）；跨域规则允许 App 直传（PUT，来源 `*`，允许头 `*`）
   - 备份桶（如 `training-backup`），私有，开服务端加密；生命周期规则：`mysql/` 前缀 30 天后删除
5. 容器镜像服务 ACR 个人版或企业版：建命名空间；把 `golang:1.27`、`gcr.io/distroless/static-debian12:nonroot`、`nginx:1.27-alpine`、`mysql:8.4`、`redis:7`、`hibiken/asynqmon:latest` 同步到 ACR（国内构建机和服务器可能拉不到 docker.io、gcr.io）。
6. 云盘快照：给两台机器的系统盘与数据库数据盘设置自动快照策略（每日，保留 7 天）。

验证：从你的电脑 `telnet <数据库机任何地址> 3306` 连不上；应用机上 `nc -zv <数据库机内网 IP> 3306` 在第 3 步之后能连上。

### 3. 数据库机

```bash
# 挂载数据盘到 /data（以 /dev/vdb 为例）
mkfs.ext4 /dev/vdb && mkdir -p /data && echo '/dev/vdb /data ext4 defaults 0 0' >> /etc/fstab && mount -a
mkdir -p /data/mysql /data/redis /data/backup /opt/training-db

# 安装 Docker（阿里云镜像源）后：
cd /opt/training-db
# 把仓库 deploy/docker-compose.db.yml、deploy/mysql/、deploy/scripts/ 拷到这里
cp .env.db.example .env   # 按模板填写：DB_PRIVATE_IP 填本机内网 IP，密码用随机串
docker compose -f docker-compose.db.yml up -d
```

建备份账号（应用账号 training 没有备份所需权限）：

```bash
docker compose -f docker-compose.db.yml exec mysql mysql -uroot -p -e "
CREATE USER 'backup'@'<应用机内网 IP>' IDENTIFIED BY '<随机密码>';
GRANT SELECT, RELOAD, LOCK TABLES, SHOW VIEW, TRIGGER, PROCESS, REPLICATION CLIENT, EVENT ON *.* TO 'backup'@'<应用机内网 IP>';
ALTER USER 'training'@'%' ACCOUNT LOCK; CREATE USER 'training'@'<应用机内网 IP>' IDENTIFIED BY '<应用账号密码>';
GRANT ALL ON training.* TO 'training'@'<应用机内网 IP>';"
```

binlog 上传（安装并配置 ossutil，使用 training-backup 子账号）：

```bash
crontab -e
*/10 * * * * BACKUP_OSS_BUCKET=training-backup /opt/training-db/scripts/backup-binlog.sh >> /var/log/binlog-backup.log 2>&1
```

验证：
- `docker compose -f docker-compose.db.yml ps` 两个服务都是 healthy / running
- 应用机上 `docker run --rm mysql:8.4 mysql -h <内网 IP> -utraining -p -e 'select 1'` 成功
- 你的电脑上连数据库机公网（如果有）或 3306 失败

### 4. 应用机

```bash
mkdir -p /opt/training/{certs,admin,nginx,scripts} /data/backup
cd /opt/training
# 安装 Docker、ossutil（training-backup 子账号）
cp <仓库>/deploy/.env.app.example .env   # 按模板填写全部值；JWT_SECRET 用 openssl rand -base64 48
echo 'BACKUP_OSS_BUCKET=training-backup' >> .env
echo 'BACKUP_MYSQL_DSN=backup:<备份账号密码>@tcp(<数据库机内网 IP>:3306)/training' >> .env
chmod 600 .env
docker login --username <ACR 用户名> registry.cn-beijing.aliyuncs.com
```

证书：在阿里云「数字证书管理服务」为 training.dreamelab.cn 申请免费 DV 证书，下载 Nginx 格式，放到
`/opt/training/certs/training.dreamelab.cn.pem` 与 `.key`。证书到期前 30 天在控制台续期并替换文件，然后
`docker compose -f docker-compose.app.yml exec nginx nginx -s reload`。

DNS：training.dreamelab.cn 的 A 记录指向应用机公网 IP。

管理后台占位页（T05 之前）：`echo '<h1>考研Training 后台</h1>' > /opt/training/admin/index.html`

每日备份：

```bash
crontab -e
30 18 * * * APP_DIR=/opt/training /opt/training/scripts/backup-mysql.sh daily >> /var/log/mysql-backup.log 2>&1
```
（UTC 18:30 = 北京时间 02:30）

验证：`dig training.dreamelab.cn` 解析到应用机；证书文件存在且权限 600。

### 5. 云效流水线

1. 云效「代码管理」建 peetraining-server 仓库，把 GitHub 暂存仓库的 `peetraining-server/` 同步过去（见仓库根目录 README-启动包.md「仓库与同步」）。
2. 云效「流水线」新建流水线，代码源选 peetraining-server 的 main 分支，触发方式：代码提交自动触发 + 手动触发。
3. 变量（私密变量勾选「加密」）：`ACR_REGISTRY`、`ACR_NAMESPACE`、`ACR_USERNAME`、`ACR_PASSWORD`、`GO_IMAGE`、`RUNTIME_IMAGE`（ACR 里同步的基础镜像）。
4. 阶段：
   - **测试**：「执行命令」任务，构建环境选带 Docker 的 Linux 构建机，命令 `bash deploy/ci/test.sh`
   - **构建镜像**：「执行命令」任务，命令 `bash deploy/ci/build-push.sh`；把仓库的 `deploy/` 目录与 `.release-version` 打包为构建产物
   - **部署**：「主机部署」任务，主机组选应用机，部署脚本：
     ```bash
     source .release-version
     bash deploy/ci/deploy.sh "$VERSION"
     ```
5. 在应用机上安装云效主机部署 Agent（按控制台提示的命令执行）。

验证：手动运行一次流水线，三个阶段都成功；`https://training.dreamelab.cn/api/v1/health` 返回 200，version 是这次的版本号。

### 6. G1 验收清单

- [ ] `curl https://training.dreamelab.cn/api/v1/health` 返回 200，`mysql`、`redis` 都是 `ok`
- [ ] 浏览器打开 https://training.dreamelab.cn/admin/ 能看到页面
- [ ] 从公网连数据库机 3306、6379 失败；从应用机能连上
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
| 健康检查 503，mysql=error | 数据库机 `docker compose ps`、磁盘 `df -h /data` | 重启 mysql；磁盘满时清理 binlog（`PURGE BINARY LOGS BEFORE NOW() - INTERVAL 3 DAY`，确认已上传 OSS） |
| 健康检查 503，redis=error | 数据库机 redis 容器日志 | 重启 redis；AOF 损坏时 `redis-check-aof --fix` |
| 502 Bad Gateway | `docker compose -f docker-compose.app.yml ps`、`logs api` | api 启动失败多为 .env 缺项（日志会写明哪个变量）；修正后 `up -d api` |
| 发布时迁移失败 | 流水线日志里的迁移报错 | 版本未切换、服务不受影响；修迁移后重新发布（不要改已合并的迁移文件，写新的） |
| 队列积压 | asynqmon 各队列 pending 数 | 看 worker 日志里失败最多的任务类型；AI 服务异常时在后台 7.8 回滚提示词版本或模型 |
| AI 成本异常 | 后台 7.8 成本统计 | 下调免费额度（7.8）或暂时关闭对应功能开关 |
| 用户付了钱没开通会员 | api 日志搜订单号：「支付回调验签失败」「支付回调开通失败」「订单重复支付」；订单表 status | 验签失败多为平台公钥或 APIv3 密钥配错；金额不符、重复支付需人工核对后在后台 7.3 处理（ADR 0011）；渠道会按自身策略重发回调，修好配置后通常自动补开通 |
| 证书过期 | 浏览器提示 | 续期并替换 certs 下的文件，`nginx -s reload` |

日志：容器日志 `docker compose logs --since 1h api`；SLS 采集应用机 `/var/lib/docker/containers/*/*-json.log`（T32 配置告警）。
日志里没有手机号明文、作答原文和资料原文，排查用户问题用请求 ID（响应头 X-Request-ID）定位。

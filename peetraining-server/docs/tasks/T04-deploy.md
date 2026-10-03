# T04 部署与发布流水线

- 仓库：后端（peetraining-server）
- 依赖：T01
- 计划完成：10/6
- 状态：审查中

## 目标

空骨架上生产，之后每张卡合并后都能直接发布验证。

## 范围

- deploy/docker-compose.app.yml（nginx、api、worker、asynqmon）与 docker-compose.db.yml（mysql、redis）
- Nginx：HTTPS、/api/v1/ 反向代理、/admin/ 静态目录、上传大小限制、健康检查
- 云效流水线配置：测试 → 构建镜像 → 推送 ACR → 备份数据库 → 执行迁移 → pull 与 up；回滚脚本
- 备份脚本：MySQL 每日全量 + binlog 上传 OSS，保留 30 天；恢复脚本
- docs/runbook.md：按顺序列出你需要在阿里云控制台、云效、两台服务器上手动完成的每一步，以及每一步如何验证

## 参考

- docs/tech-plan.md「架构」「环境与发布」

## 验收

- G1：https://training.dreamelab.cn/api/v1/health 正常；/admin/ 能打开（可先放占位页）
- MySQL 与 Redis 从公网连不上，从应用机能连上
- 手动跑一次备份，OSS 里有文件，并在本地用它恢复成功
- 改一行代码走一遍流水线，发布与回滚各成功一次

## 注意

所有密钥在文件里用占位符，不要向我索要真实密钥；需要我在控制台或服务器上操作的，写进 runbook 并在自检里列出。

## 不做

- 卡片范围以外的页面、接口和重构；发现需要的，记到 docs/open-questions.md

## 记录（开发中填写）

- 实现要点：
  - deploy/docker-compose.app.yml（nginx、api、worker、asynqmon 只绑 127.0.0.1）与 docker-compose.db.yml（mysql、redis 只绑内网 IP，开 binlog 与 AOF）
  - Nginx：HTTP 跳 HTTPS、/api/ 反向代理（Docker DNS 动态解析，api 重建后不 502）、/admin/ 单页应用回退、访问日志不记查询参数、请求体上限 2 MB
  - 发布脚本 release.sh：发布前备份 → 拉镜像 → 迁移（失败不切版本）→ 重启 → 健康检查，失败自动回滚；rollback.sh 回到上一版本或指定版本
  - 备份：backup-mysql.sh（单独的 backup 账号、失败时删除残缺文件、上传 OSS、本地留 7 天）、backup-binlog.sh（每 10 分钟上传已轮转的 binlog）、restore-mysql.sh
  - 云效流水线不写平台专用 YAML，三个阶段都调用仓库里的脚本（ci/test.sh、ci/build-push.sh、ci/deploy.sh），在控制台按 runbook 第 5 步配置
  - docs/runbook.md：首次搭建 6 步（每步有验证方法）、G1 验收清单、发布回滚、备份恢复演练、故障排查
- 偏离计划的地方与原因：
  - 修了 T01 Dockerfile 的一个问题：ENTRYPOINT 与 compose 的 command 叠加导致 api 起不来，改为 CMD（冒烟测试发现）
  - 应用账号没有 mysqldump 需要的 RELOAD 等权限，备份改用单独的 backup 账号（BACKUP_MYSQL_DSN）
- 在开发环境里已验证：
  - 两个 compose 文件 `docker compose config` 通过；Nginx 配置 `nginx -t` 通过；脚本 shellcheck 无警告
  - 本地起 MySQL：迁移 → 写一行数据 → backup-mysql.sh 备份 → restore-mysql.sh 恢复到另一个库，数据、迁移版本、规则参数都在；备份失败时不留残缺文件
  - 用 compose 起整套（MySQL、Redis、api、worker、nginx + 自签证书）：HTTPS 健康检查 200、/admin/ 单页回退、未实现接口 501 经 Nginx 正常返回、重启 api 后不 502
- 需要手动验证的步骤（runbook 第一部分，需要你在控制台与服务器上操作）：
  - 第 1–5 步：RAM 子账号、VPC 与安全组、两台 ECS、OSS 两个桶、ACR 与基础镜像同步、证书与 DNS、两台机器的 .env、云效流水线
  - 第 6 步 G1 清单逐项打勾
- 验收结果：
  - [ ] G1 health 正常、/admin/ 能打开——需要服务器就绪后验证（本地整套冒烟已通过）
  - [ ] MySQL 与 Redis 公网连不上、应用机能连上——需要服务器验证（compose 只绑内网 IP）
  - [ ] 手动备份上传 OSS 并本地恢复——恢复流程已在本地验证，OSS 上传需要服务器验证
  - [ ] 流水线发布与回滚各一次——需要云效配置后验证

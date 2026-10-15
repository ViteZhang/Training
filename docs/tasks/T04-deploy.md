# T04 部署与发布流水线

- 仓库：后端（peetraining-server）
- 依赖：T01
- 计划完成：10/6
- 状态：审查中

## 目标

空骨架上生产，之后每张卡合并后都能直接发布验证。

## 范围

- deploy/docker-compose.yml（接入现有 dreamer-net 的 api、worker、asynqmon）
- 现有 Nginx：HTTPS、/api/v1/ 反向代理、/admin/ 静态目录、上传大小限制、健康检查；本项目不再提供 Nginx 配置
- 云效流水线配置：Go 构建 → 构建镜像 → 推送 ACR → 在 ECS 执行 `docker-compose up -d`；数据库迁移在发布前单独完成；镜像固定使用 `latest`
- docs/runbook.md：按顺序列出你需要在阿里云控制台、云效、两台服务器上手动完成的每一步，以及每一步如何验证

## 参考

- docs/tech-plan.md「架构」「环境与发布」

## 验收

- G1：https://training.dreamelab.cn/api/v1/health 正常；/admin/ 能打开（可先放占位页）
- MySQL 与 Redis 从公网连不上，从应用机能连上
- 改一行代码走一遍流水线，发布成功并通过健康检查

## 注意

所有密钥在文件里用占位符，不要向我索要真实密钥；需要我在控制台或服务器上操作的，写进 runbook 并在自检里列出。

## 不做

- 卡片范围以外的页面、接口和重构；发现需要的，记到 docs/open-questions.md

## 记录（开发中填写）

- 实现要点：
  - deploy/docker-compose.yml（api、worker、asynqmon 加入现有 dreamer-net）；MySQL、Redis、Nginx 沿用 ECS 现有容器
  - Nginx：HTTP 跳 HTTPS、/api/ 反向代理（Docker DNS 动态解析，api 重建后不 502）、/admin/ 单页应用回退、访问日志不记查询参数、请求体上限 2 MB
  - Compose 的 pull_policy 每次拉取 latest；API 健康探测由镜像内的 `/app/api healthcheck` 执行
  - 云效流水线按 runbook 第 5 步配置：Go 原生构建任务编译二进制，原生 Docker 任务构建并推送 ACR，主机部署任务执行 `cd /opt/dreamer/project/training && docker-compose up -d`
  - docs/runbook.md：首次搭建（每步有验证方法）、G1 验收清单、发布、故障排查
- 偏离计划的地方与原因：
  - 修了 T01 Dockerfile 的一个问题：ENTRYPOINT 与 compose 的 command 叠加导致 api 起不来，改为 CMD（冒烟测试发现）
- 在开发环境里已验证：
  - 应用 Compose 配置通过；现有 Nginx 配置由 ECS 自己验证
  - 本地起 MySQL：迁移后写入并读取测试数据，确认迁移结果与规则参数可用
  - 用现有 MySQL、Redis、Nginx 加项目 api、worker、asynqmon：HTTPS 健康检查 200、/admin/ 单页回退、重启 api 后不 502
- 需要手动验证的步骤（runbook 第一部分，需要你在控制台与服务器上操作）：
  - 第 1–5 步：RAM 子账号、VPC 与安全组、两台 ECS、业务 OSS 桶、ACR 与基础镜像同步、证书与 DNS、两台机器的 .env、云效流水线
  - 第 6 步 G1 清单逐项打勾
- 验收结果：
  - [ ] G1 health 正常、/admin/ 能打开——需要服务器就绪后验证（本地整套冒烟已通过）
  - [ ] MySQL 与 Redis 公网连不上、应用机能连上——需要服务器验证（compose 只绑内网 IP）
  - [ ] 流水线发布一次并通过健康检查——需要云效配置后验证

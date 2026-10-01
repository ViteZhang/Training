# T04 部署与发布流水线

- 仓库：后端（peetraining-server）
- 依赖：T01
- 计划完成：10/6
- 状态：未开始

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
- 偏离计划的地方与原因：
- 需要手动验证的步骤：

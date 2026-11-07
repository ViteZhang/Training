# Training 仓库 · 三端同步规则

本仓库（GitHub）有两个目录，分别对应云效上的两个正式仓库，各自同步云效上的一个分支：

| 目录 | 云效仓库 | 同步的云效分支 | 推到该分支的后果 |
| --- | --- | --- | --- |
| peetraining-server/ | peetraining-server | main | 流水线构建并发布 API、Worker 到生产 |
| peetraining-web/ | peetraining-web | master-zhangzhao | App 源码（发布方式见该目录的文档） |

- web 从 2026-11-04 起改用同事建的 `master-zhangzhao` 分支（带连字符）：云效 web 上的改动只合并到这个分支，不再合并或推送到云效 web 的 main
- GitHub 一侧照旧：所有改动开 PR 合并到 GitHub 的 main，并与云效保持一致（以云效为准）
- 分支对应关系写在 `scripts/sync-codeup.sh` 的 `branch_of` 里，要改分支就改那里
- 管理后台已从 web 拆到云效单独的 `peetraining-admin` 仓库（分支 master），不在本仓库里，也不在同步范围内

各目录的开发规范见各自的 CLAUDE.md。

## 生产服务器（2026-11 起）

- 阿里云 47.95.34.95（入口 + 数据 + 前端）：nginx（域名、TLS、备案、反代）、MySQL / Redis / Qdrant（全部业务数据）、training-admin、dreamer-web、camera、hair 等；WireGuard `10.88.0.1`
- 京东云 111.228.45.235（后端计算）：training-api / training-worker、dreamer-server、dreamer-interview-api / worker、dreamer-agent
- 两边走 WireGuard 虚拟隧道（私网 `10.88.0.0/24`）：京东云经 `10.88.0.1` 访问阿里云数据库，阿里云 Nginx 经隧道反代京东云后端接口
- 生产数据库迁移仍在阿里云 ECS 上执行（`dreamer-mysql`）；应用 `.env`、`docker compose`、云效部署在京东云应用机。详见 peetraining-server/docs/runbook.md「生产拓扑」

## 用户偏好

- 回复一律用中文
- AI 模型统一用 qwen3.8-flash（不再用 qwen-max、qwen-plus），百炼默认不开思考（AI_BAILIAN_THINKING=false）；
  评测也用它：`AI_PROVIDER=bailian go run ./cmd/eval -cap=…`（代码默认模型已是 qwen3.8-flash）。生产以京东云应用机 .env 为准，见 peetraining-server/docs/runbook.md 第 8 节

## 三端：Claude 会话的工作副本、GitHub、云效，三者内容保持一致（以云效为准）

云效是正式来源（打包、发布都用云效），GitHub 跟随云效保持一致。
（2026-11-04 曾短暂改为不同步 GitHub，当天恢复。）

目录与云效仓库用 git subtree 关联，历史已接上，用 `scripts/sync-codeup.sh` 同步（凭证读环境变量 CODEUP_ORG_URL、CODEUP_USERNAME、CODEUP_TOKEN）：

1. 开工前：`scripts/sync-codeup.sh status`；云效有新提交（同事可能直接在云效提交）就先 `scripts/sync-codeup.sh pull`，合并进当前分支
   - 脚本会自动补全浅克隆的历史；云效严格领先时出现的冲突（subtree 合并基点较旧所致）脚本会自动取云效版本完成合并；
     只有两边都改了同一处时才需要人工解决
   - 只是把云效最新拉到 GitHub 时：pull 后开 PR 合并到 GitHub main 即可，不需要往云效推
2. 开发、测试、提交到 GitHub 分支，开 PR；**PR 用「Create a merge commit」合并，不要 Squash 或 Rebase**（会断开与云效的历史关联）
3. 合并后在 GitHub main 上再执行一次 `status`、`pull`，然后 `scripts/sync-codeup.sh push server`（推到云效 server 的 main）/ `push web`（推到云效 web 的 master-zhangzhao）
   - 推完 server 后流水线自动运行（云效 main 推送触发）：`scripts/yunxiao-flow.sh runs` 找到这次的运行编号，再 `scripts/yunxiao-flow.sh wait <编号>` 等到构建、部署都成功；需要重新发布时用 `scripts/yunxiao-flow.sh run`
4. 推到云效 server 的 main 会发布到生产：推送前先跟用户确认；有新的数据库迁移时，提醒用户按 peetraining-server/docs/runbook.md 先在生产库执行迁移
   - 给用户生产迁移时，直接给出可以在阿里云 ECS（数据库所在机器）上粘贴执行的完整 SQL：先 `SET NAMES utf8mb4;`，再按编号从小到大放每个迁移文件 `-- +goose Up` 部分的全文，每个文件后面紧跟 `INSERT INTO goose_db_version (version_id, is_applied) VALUES (N, 1);`；
     连同进入 MySQL 的命令（`docker exec -it dreamer-mysql mysql -uroot -p --default-character-set=utf8mb4 training`）、执行前查版本号与执行后核对的 SQL 一起给。
     不要让用户在本机仓库里生成 SQL、scp 文件或跑脚本；给之前先在 testcontainers 上验证这份 SQL 能从上一个版本执行到最新
   - 粘贴进 ECS 上 mysql 客户端时中文会被吞掉（2026-11-03 迁移 00037–00041 时出现过：课程名、方向名变成空串）：给用户的 SQL 必须全是 ASCII，
     中文一律写成 `CONVERT(X'E59B9B…' USING utf8mb4)`（UTF-8 十六进制），表和列的中文 COMMENT 可以省略；给之前检查一遍没有非 ASCII 字符

不要做的事：
- 不要强推云效（server 的 main、web 的 master-zhangzhao）或 GitHub 的 main；脚本提示「两边都有新提交」时先 pull 合并
- 不要往云效 web 的 main 推送或合并
- 不要手工把云效仓库的内容整体复制覆盖本目录（会丢掉对方的改动并断开历史）；脚本在云效严格领先时取云效版本是合并提交，不在此列
- 令牌只从环境变量读，不写进命令行、URL、git 配置、日志或提交信息

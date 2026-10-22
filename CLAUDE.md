# Training 仓库 · 三端同步规则

本仓库（GitHub）有两个目录，分别对应云效上的两个正式仓库，流水线从云效 main 发布：

| 目录 | 云效仓库 | 推到云效 main 的后果 |
| --- | --- | --- |
| peetraining-server/ | peetraining-server | 流水线构建并发布 API、Worker 到生产 |
| peetraining-web/ | peetraining-web | App 与管理后台源码（发布方式见该目录的文档） |

各目录的开发规范见各自的 CLAUDE.md。

## 三端：Claude 会话的工作副本、GitHub、云效，三者内容保持一致

目录与云效仓库用 git subtree 关联，历史已接上，用 `scripts/sync-codeup.sh` 同步（凭证读环境变量 CODEUP_ORG_URL、CODEUP_USERNAME、CODEUP_TOKEN）：

1. 开工前：`scripts/sync-codeup.sh status`；云效有新提交（同事可能直接在云效提交）就先 `scripts/sync-codeup.sh pull`，合并进当前分支
2. 开发、测试、提交到 GitHub 分支，开 PR；**PR 用「Create a merge commit」合并，不要 Squash 或 Rebase**（会断开与云效的历史关联）
3. 合并后在 main 上再执行一次 `status`、`pull`，然后 `scripts/sync-codeup.sh push server` / `push web`
4. 推到云效 server 的 main 会发布到生产：推送前先跟用户确认；有新的数据库迁移时，提醒用户按 peetraining-server/docs/runbook.md 先在生产库执行迁移

不要做的事：
- 不要强推云效或 GitHub 的 main；脚本提示「两边都有新提交」时先 pull 合并
- 不要把云效仓库的内容整体复制覆盖本目录（会丢掉对方的改动并断开历史）
- 令牌只从环境变量读，不写进命令行、URL、git 配置、日志或提交信息

#!/usr/bin/env bash
# 发布 latest 镜像（流水线最后一步在应用机执行）：
#   拉镜像 → 重启 api 与 worker → 健康检查。
# 数据库迁移由发布前单独完成，服务发布阶段不修改数据库结构。
source "$(dirname "$0")/common.sh"

log "发布 latest 镜像"

${COMPOSE} pull api worker
${COMPOSE} up -d --no-deps api worker

if wait_healthy; then
  log "发布成功：latest"
else
  log "健康检查失败，停止本次启动的 API/Worker；请查看容器日志后重新发布"
  ${COMPOSE} stop api worker
  exit 1
fi

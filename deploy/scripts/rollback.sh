#!/usr/bin/env bash
# 回滚到上一版本：把镜像版本号改回 PREVIOUS_VERSION 并重启 api 与 worker。
# 也可以指定版本：rollback.sh v0.1.1
source "$(dirname "$0")/common.sh"

TARGET=${1:-$(cat "${APP_DIR}/PREVIOUS_VERSION")}
CUR=$(env_get VERSION)
log "回滚 ${CUR} → ${TARGET}"
sed -i "s/^VERSION=.*/VERSION=${TARGET}/" "${APP_DIR}/.env"
${COMPOSE} pull api worker
${COMPOSE} up -d --no-deps api worker
if wait_healthy; then
  log "回滚完成：${TARGET}"
else
  log "回滚后健康检查仍失败，请按 runbook「故障排查」处理"
  exit 1
fi

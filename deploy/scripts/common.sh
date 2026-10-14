#!/usr/bin/env bash
# 发布脚本共用的设置。在应用机 /opt/training 下执行。
set -euo pipefail

APP_DIR=${APP_DIR:-/opt/training}
COMPOSE="docker compose -f ${APP_DIR}/docker-compose.app.yml --env-file ${APP_DIR}/.env"
log() { printf '%s %s\n' "$(date '+%F %T')" "$*"; }

# 等健康检查通过：https://域名/api/v1/health 返回 200
wait_healthy() {
  local url=${HEALTH_URL:-https://training.dreamelab.cn/api/v1/health}
  for _ in $(seq 1 30); do
    if curl -fsS --max-time 3 "$url" >/dev/null; then return 0; fi
    sleep 2
  done
  return 1
}

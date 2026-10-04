#!/usr/bin/env bash
# 发布、回滚、备份脚本共用的设置。在应用机 /opt/training 下执行。
set -euo pipefail

APP_DIR=${APP_DIR:-/opt/training}
COMPOSE="docker compose -f ${APP_DIR}/docker-compose.app.yml --env-file ${APP_DIR}/.env"
BACKUP_DIR=${BACKUP_DIR:-/data/backup}

log() { printf '%s %s\n' "$(date '+%F %T')" "$*"; }

# 读取 .env 里的变量（只读需要的几项，不 source 整个文件，避免密钥进入 shell 历史）
env_get() { { grep -E "^$1=" "${APP_DIR}/.env" || true; } | tail -1 | cut -d= -f2-; }

# 从 DSN（user:pass@tcp(host:port)/db）解析连接参数；参数为 .env 里的变量名，默认 MYSQL_DSN
parse_dsn() {
  local dsn; dsn=$(env_get "${1:-MYSQL_DSN}")
  DB_USER=${dsn%%:*}
  local rest=${dsn#*:}
  DB_PASS=${rest%%@tcp(*}
  local hostport=${dsn#*@tcp(}; hostport=${hostport%%)*}
  DB_HOST=${hostport%%:*}
  DB_PORT=${hostport##*:}
  DB_NAME=${dsn##*/}; DB_NAME=${DB_NAME%%\?*}
}

# 等健康检查通过：https://域名/api/v1/health 返回 200
wait_healthy() {
  local url=${HEALTH_URL:-https://training.dreamelab.cn/api/v1/health}
  for _ in $(seq 1 30); do
    if curl -fsS --max-time 3 "$url" >/dev/null; then return 0; fi
    sleep 2
  done
  return 1
}

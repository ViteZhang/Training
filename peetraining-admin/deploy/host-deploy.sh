#!/usr/bin/env bash
# 云效「主机部署」脚本：在应用机执行，更新 training-admin 容器。
# 前提：
#   1. /opt/dreamer/project/training-admin 下已有 docker-compose.yml 和 .env
#   2. 应用机已 docker login ACR（一次性）
#   3. 前一阶段已把新镜像推送到 ACR
set -eu

APP_DIR=${APP_DIR:-/opt/dreamer/project/training-admin}
SERVICE=admin
CONTAINER=training-admin

cd "$APP_DIR"

docker compose pull "$SERVICE"
docker compose up -d "$SERVICE"

# 等容器健康检查通过（compose healthcheck 指向 /healthz）。
i=0
while [ "$i" -lt 12 ]; do
  if docker exec "$CONTAINER" wget -qO- http://127.0.0.1/healthz >/dev/null 2>&1; then
    echo "training-admin healthy"
    docker compose ps "$SERVICE"
    exit 0
  fi
  sleep 5
  i=$((i + 1))
done

echo "training-admin health check failed"
docker compose logs --tail=50 "$SERVICE"
exit 1

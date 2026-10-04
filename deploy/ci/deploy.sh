#!/usr/bin/env bash
# 云效流水线「部署」阶段（主机部署任务在应用机上执行）：同步部署文件，然后发布。
# 云效「主机部署」会把制品解压到工作目录；这里把 deploy/ 下的 compose、nginx、脚本同步到 /opt/training，
# 不覆盖服务器上的 .env、certs、admin。
set -euo pipefail
APP_DIR=${APP_DIR:-/opt/training}
VERSION=${1:?用法：deploy.sh <版本号>}
SRC=$(cd "$(dirname "$0")/.." && pwd)

mkdir -p "${APP_DIR}"
cp "${SRC}/docker-compose.app.yml" "${APP_DIR}/"
mkdir -p "${APP_DIR}/nginx" "${APP_DIR}/scripts"
cp "${SRC}/nginx/training.conf" "${APP_DIR}/nginx/"
cp "${SRC}"/scripts/*.sh "${APP_DIR}/scripts/"
chmod +x "${APP_DIR}"/scripts/*.sh

# 登录 ACR 拉镜像（应用机上已执行过 docker login 的可省略）
if [ -n "${ACR_PASSWORD:-}" ]; then
  echo "${ACR_PASSWORD}" | docker login --username "${ACR_USERNAME}" --password-stdin "${ACR_REGISTRY}"
fi
export APP_DIR
"${APP_DIR}/scripts/release.sh" "${VERSION}"
# 配置变化时让 Nginx 重新加载
docker compose -f "${APP_DIR}/docker-compose.app.yml" --env-file "${APP_DIR}/.env" exec -T nginx nginx -s reload || true

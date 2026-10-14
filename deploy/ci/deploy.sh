#!/usr/bin/env bash
# 云效 Docker 部署节点在 ECS 上执行：bash deploy/ci/deploy.sh。
# 制品解压到工作目录后，需保留 deploy/ci/、deploy/scripts/ 和 deploy/docker-compose.app.yml 的路径。
# 这里同步应用 Compose 和运维脚本到 /opt/training。
# ECS 已有 dreamer-nginx，不覆盖服务器上的 .env、证书和前端文件。
set -euo pipefail
APP_DIR=${APP_DIR:-/opt/training}
SRC=$(cd "$(dirname "$0")/.." && pwd)

[[ -f "${SRC}/docker-compose.app.yml" && -d "${SRC}/scripts" ]] || { echo "部署制品缺少 Compose 文件或脚本：$SRC" >&2; exit 2; }
[[ -f "${APP_DIR}/.env" ]] || { echo "缺少服务器配置：${APP_DIR}/.env" >&2; exit 2; }

mkdir -p "${APP_DIR}"
cp "${SRC}/docker-compose.app.yml" "${APP_DIR}/"
mkdir -p "${APP_DIR}/scripts"
cp "${SRC}"/scripts/*.sh "${APP_DIR}/scripts/"
chmod +x "${APP_DIR}"/scripts/*.sh
# 清理旧版本遗留的回滚脚本；当前发布固定使用 latest，不保留版本回滚入口。
rm -f "${APP_DIR}/scripts/rollback.sh"

docker network inspect dreamer-net >/dev/null

# 登录 ACR 拉镜像（应用机上已执行过 docker login 的可省略）
if [[ -n "${ACR_REGISTRY:-}${ACR_USERNAME:-}${ACR_PASSWORD:-}" ]]; then
  : "${ACR_REGISTRY:?设置 ACR_REGISTRY，或在应用机预先 docker login}"
  : "${ACR_USERNAME:?设置 ACR_USERNAME}"
  : "${ACR_PASSWORD:?设置 ACR_PASSWORD}"
  echo "${ACR_PASSWORD}" | docker login --username "${ACR_USERNAME}" --password-stdin "${ACR_REGISTRY}"
fi
export APP_DIR
"${APP_DIR}/scripts/release.sh"

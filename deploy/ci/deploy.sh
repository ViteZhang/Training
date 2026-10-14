#!/usr/bin/env bash
# 云效 Docker 部署节点在 ECS 上执行：bash deploy/ci/deploy.sh "$VERSION"。
# 制品解压到工作目录后，需保留 deploy/ci/、deploy/scripts/ 和 deploy/docker-compose.app.yml 的路径。
# 这里同步应用 Compose 和运维脚本到 /opt/training。
# ECS 已有 dreamer-nginx，不覆盖服务器上的 .env、证书和前端文件。
set -euo pipefail
APP_DIR=${APP_DIR:-/opt/training}
VERSION=${1:-${VERSION:-}}
if [[ -z "$VERSION" && -f .release-version ]]; then
  VERSION=$(awk -F= '$1 == "VERSION" { value = substr($0, index($0, "=") + 1) } END { print value }' .release-version)
fi
if [[ -z "$VERSION" ]]; then
  printf '%s\n' '缺少 VERSION；请传入版本号或在当前目录提供 .release-version。' >&2
  exit 2
fi
SRC=$(cd "$(dirname "$0")/.." && pwd)

if [[ ! "$VERSION" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]]; then
  printf 'VERSION 不是合法的 Docker 镜像标签：%s\n' "$VERSION" >&2
  exit 2
fi
if [[ ! -f "${SRC}/docker-compose.app.yml" || ! -d "${SRC}/scripts" ]]; then
  printf '部署制品缺少 deploy/docker-compose.app.yml 或 deploy/scripts/：%s\n' "$SRC" >&2
  exit 2
fi
if [[ ! -f "${APP_DIR}/.env" ]]; then
  printf '缺少服务器配置文件：%s/.env；请先按 deploy/.env.app.example 配置并 chmod 600。\n' "$APP_DIR" >&2
  exit 2
fi
env_value() {
  local key=$1
  awk -F= -v key="$key" '$1 == key { value = substr($0, index($0, "=") + 1) } END { print value }' "${APP_DIR}/.env"
}
if [[ "$(env_value APP_ENV)" != production ]]; then
  printf '%s\n' '服务器 .env 必须设置 APP_ENV=production。' >&2
  exit 2
fi
for key in IMAGE VERSION MYSQL_DSN REDIS_ADDR JWT_SECRET; do
  value=$(env_value "$key")
  if [[ -z "$value" || "$value" == *CHANGE_ME* || "$value" == *'<'* ]]; then
    printf '服务器 .env 尚未正确配置：%s\n' "$key" >&2
    exit 2
  fi
done
jwt_secret=$(env_value JWT_SECRET)
if ((${#jwt_secret} < 32)); then
  printf '%s\n' '服务器 .env 的 JWT_SECRET 至少需要 32 个字符。' >&2
  exit 2
fi

mkdir -p "${APP_DIR}"
cp "${SRC}/docker-compose.app.yml" "${APP_DIR}/"
mkdir -p "${APP_DIR}/scripts"
cp "${SRC}"/scripts/*.sh "${APP_DIR}/scripts/"
chmod +x "${APP_DIR}"/scripts/*.sh

docker network inspect dreamer-net >/dev/null

# 登录 ACR 拉镜像（应用机上已执行过 docker login 的可省略）
if [[ -n "${ACR_REGISTRY:-}${ACR_USERNAME:-}${ACR_PASSWORD:-}" ]]; then
  : "${ACR_REGISTRY:?设置 ACR_REGISTRY，或在应用机预先 docker login}"
  : "${ACR_USERNAME:?设置 ACR_USERNAME}"
  : "${ACR_PASSWORD:?设置 ACR_PASSWORD}"
  echo "${ACR_PASSWORD}" | docker login --username "${ACR_USERNAME}" --password-stdin "${ACR_REGISTRY}"
fi
export APP_DIR
"${APP_DIR}/scripts/release.sh" "${VERSION}"

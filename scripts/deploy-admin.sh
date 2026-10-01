#!/usr/bin/env bash
# 构建管理后台并同步到应用机 Nginx 的 /admin/ 目录（云效流水线「主机部署」或本机手动执行）。
# 用法：scripts/deploy-admin.sh [应用机 SSH 地址]
#   在应用机上由云效主机部署执行时不传参数，直接同步到本机 /opt/training/admin。
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
TARGET_DIR=${ADMIN_DIR:-/opt/training/admin}

if [ ! -d "${ROOT}/apps/admin/dist" ] || [ "${SKIP_BUILD:-0}" != "1" ]; then
  (cd "${ROOT}" && pnpm install --frozen-lockfile && pnpm --filter admin build)
fi

if [ -n "${1:-}" ]; then
  rsync -az --delete "${ROOT}/apps/admin/dist/" "$1:${TARGET_DIR}/"
else
  mkdir -p "${TARGET_DIR}"
  rsync -a --delete "${ROOT}/apps/admin/dist/" "${TARGET_DIR}/"
fi
echo "后台已同步到 ${1:-本机}:${TARGET_DIR}"

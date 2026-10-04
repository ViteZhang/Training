#!/usr/bin/env bash
# 发布新版本（流水线最后一步通过 SSH 在应用机执行）：
#   备份数据库 → 拉镜像 → 执行迁移 → 重启 api 与 worker → 健康检查；健康检查失败自动回滚到上一版本。
# 用法：deploy/scripts/release.sh v0.1.3
# 迁移只加不删（CLAUDE.md 必须遵守第 3 条），所以回滚镜像时不需要回滚数据库。
source "$(dirname "$0")/common.sh"

NEW=${1:?用法：release.sh <版本号>}
CUR=$(env_get VERSION)
log "当前版本 ${CUR}，发布 ${NEW}"

"$(dirname "$0")/backup-mysql.sh" "pre-${NEW}"

sed -i "s/^VERSION=.*/VERSION=${NEW}/" "${APP_DIR}/.env"
echo "${CUR}" > "${APP_DIR}/PREVIOUS_VERSION"

${COMPOSE} pull api worker
log "执行迁移"
if ! ${COMPOSE} run --rm --no-deps api /app/api migrate up; then
  log "迁移失败，恢复版本号 ${CUR}，服务未变更"
  sed -i "s/^VERSION=.*/VERSION=${CUR}/" "${APP_DIR}/.env"
  exit 1
fi

${COMPOSE} up -d --no-deps api worker
${COMPOSE} up -d nginx asynqmon

if wait_healthy; then
  log "发布成功：${NEW}"
else
  log "健康检查失败，自动回滚到 ${CUR}"
  "$(dirname "$0")/rollback.sh"
  exit 1
fi

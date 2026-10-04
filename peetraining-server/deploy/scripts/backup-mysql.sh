#!/usr/bin/env bash
# MySQL 全量备份并上传 OSS（每日定时，以及每次发布前）。
#   用 mysql 镜像里的 mysqldump，--single-transaction 不锁表；压缩后上传 oss://<备份桶>/mysql/full/
#   OSS 生命周期规则保留 30 天（runbook 第 2 步）。
# 用法：backup-mysql.sh [标签]   例：backup-mysql.sh daily
source "$(dirname "$0")/common.sh"
# 备份用单独的只读账号 backup（需要 SELECT、RELOAD、LOCK TABLES、SHOW VIEW、TRIGGER、PROCESS、REPLICATION CLIENT 权限，
# runbook 第 3 步创建），连接串放在 BACKUP_MYSQL_DSN；应用账号没有这些权限。
if [ -n "$(env_get BACKUP_MYSQL_DSN)" ]; then parse_dsn BACKUP_MYSQL_DSN; else parse_dsn MYSQL_DSN; fi

TAG=${1:-manual}
BUCKET=$(env_get BACKUP_OSS_BUCKET)
mkdir -p "${BACKUP_DIR}"
FILE="${BACKUP_DIR}/training-$(date -u +%Y%m%dT%H%M%SZ)-${TAG}.sql.gz"

log "备份到 ${FILE}"
trap 'rm -f "${FILE}"; log "备份失败"' ERR
IMG=$(env_get BACKUP_MYSQL_IMAGE); IMG=${IMG:-mysql:8.4}
docker run --rm --network host -e MYSQL_PWD="${DB_PASS}" "${IMG}" \
  mysqldump -h "${DB_HOST}" -P "${DB_PORT}" -u "${DB_USER}" \
  --single-transaction --routines --triggers --set-gtid-purged=OFF --source-data=2 "${DB_NAME}" \
  | gzip > "${FILE}"

test -s "${FILE}" || { log "备份文件为空"; exit 1; }
if [ -n "${BUCKET}" ]; then
  ossutil cp "${FILE}" "oss://${BUCKET}/mysql/full/$(basename "${FILE}")"
  log "已上传 oss://${BUCKET}/mysql/full/"
else
  log "未设置 BACKUP_OSS_BUCKET，只保留本地文件"
fi
# 本地只留 7 天
find "${BACKUP_DIR}" -name 'training-*.sql.gz' -mtime +7 -delete

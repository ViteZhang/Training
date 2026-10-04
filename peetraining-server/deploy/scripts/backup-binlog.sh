#!/usr/bin/env bash
# 持续上传 MySQL binlog 到 OSS（数据库机上每 10 分钟由 cron 执行），配合全量备份做按时间点恢复。
# 只上传已经轮转完成的 binlog（当前正在写的那个不传）。
set -euo pipefail
BUCKET=${BACKUP_OSS_BUCKET:?需要设置 BACKUP_OSS_BUCKET}
BINLOG_DIR=${BINLOG_DIR:-/data/mysql}
STATE=${STATE:-/data/backup/binlog-uploaded.txt}
mkdir -p "$(dirname "${STATE}")"; touch "${STATE}"

mapfile -t logs < <(ls -1 "${BINLOG_DIR}"/mysql-bin.[0-9]* 2>/dev/null | sort)
(( ${#logs[@]} > 1 )) || exit 0
for f in "${logs[@]:0:${#logs[@]}-1}"; do
  name=$(basename "$f")
  grep -qx "${name}" "${STATE}" && continue
  ossutil cp "$f" "oss://${BUCKET}/mysql/binlog/${name}" && echo "${name}" >> "${STATE}"
done

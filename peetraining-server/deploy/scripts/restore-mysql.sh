#!/usr/bin/env bash
# 用全量备份恢复到一个 MySQL（每月演练时恢复到本地或临时库，不要直接对生产库执行）。
# 用法：restore-mysql.sh <备份文件 .sql.gz> <目标 host> <端口> <用户> <库名>
#   密码从环境变量 MYSQL_PWD 读取。按时间点恢复：先恢复全量，再用 mysqlbinlog 回放
#   备份文件头部 CHANGE MASTER 注释里记录的位置之后的 binlog（见 runbook「备份恢复」）。
set -euo pipefail
FILE=${1:?备份文件}; HOST=${2:?host}; PORT=${3:-3306}; USER=${4:-root}; DB=${5:-training}
: "${MYSQL_PWD:?需要设置 MYSQL_PWD}"
echo "恢复 ${FILE} → ${USER}@${HOST}:${PORT}/${DB}"
gunzip -c "${FILE}" | docker run --rm -i --network host -e MYSQL_PWD mysql:8.4 \
  sh -c "mysql -h ${HOST} -P ${PORT} -u ${USER} -e 'CREATE DATABASE IF NOT EXISTS \`${DB}\` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci' && mysql -h ${HOST} -P ${PORT} -u ${USER} ${DB}"
echo "恢复完成。核对：SELECT COUNT(*) FROM users; 与最近的迁移版本 SELECT MAX(version_id) FROM goose_db_version;"

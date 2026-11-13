#!/usr/bin/env bash
# 在阿里云入口机上发布 H5 版：解压构建产物，用一个独立的 Nginx 容器 training-h5 提供网页。
# 只动自己的目录（/opt/training-h5）和自己的容器（training-h5），不碰 dreamer-nginx、后端和 App。
#
#   deploy.sh <构建产物 tgz>      发布（tgz 内含 dist-h5/ 与 nginx.conf）
#   deploy.sh rollback           回到上一个版本
#
# 云效流水线「peetraining-h5上线」的主机部署步骤调用它；也可以手工上传 tgz 后执行。
set -euo pipefail

BASE=/opt/training-h5
NAME=training-h5
NETWORK=${H5_NETWORK:-dreamer-net}
# 默认复用入口 Nginx 的镜像，机器上已经有，不用再拉
IMAGE=${H5_NGINX_IMAGE:-$(docker inspect -f '{{.Config.Image}}' dreamer-nginx 2>/dev/null || echo nginx:alpine)}

mkdir -p "$BASE/releases"

# 用指定版本目录重建容器并等它健康。容器直接挂版本目录（不是 current 链接），所以换版本必须重建容器。
start() {
  local rel=$1 i=0
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  docker run -d --name "$NAME" --restart unless-stopped \
    --network "$NETWORK" --network-alias "$NAME" \
    -v "$rel/dist-h5:/usr/share/nginx/html/h5:ro" \
    -v "$rel/nginx.conf:/etc/nginx/conf.d/default.conf:ro" \
    "$IMAGE" >/dev/null
  until docker exec "$NAME" sh -c 'wget -qO- http://127.0.0.1/healthz || curl -fsS http://127.0.0.1/healthz' >/dev/null 2>&1; do
    i=$((i + 1))
    if [[ $i -ge 12 ]]; then echo "$NAME 健康检查失败" >&2; docker logs --tail=50 "$NAME" >&2; return 1; fi
    sleep 5
  done
  ln -sfn "$rel" "$BASE/current"
}

if [[ ${1:-} == rollback ]]; then
  cur=$(readlink -f "$BASE/current" 2>/dev/null || true)
  prev=$(ls -1d "$BASE"/releases/*/ 2>/dev/null | sed 's#/$##' | sort | grep -vx "$cur" | awk -v c="$cur" '$0 < c' | tail -1)
  if [[ -z $prev ]]; then echo "没有比当前更早的版本可回退" >&2; exit 1; fi
  start "$prev"
  echo "已回退到 $prev：$(cat "$prev/dist-h5/version.json" 2>/dev/null)"
  exit 0
fi

PKG=${1:?用法：deploy.sh <构建产物 tgz> | rollback}
REL="$BASE/releases/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$REL"
tar -xzf "$PKG" -C "$REL"
test -f "$REL/dist-h5/index.html" || { echo "产物里没有 dist-h5/index.html" >&2; rm -rf "$REL"; exit 1; }
test -f "$REL/nginx.conf" || { echo "产物里没有 nginx.conf" >&2; rm -rf "$REL"; exit 1; }

PREV=$(readlink -f "$BASE/current" 2>/dev/null || true)
if ! start "$REL"; then
  # 新版本起不来：退回上一个能用的版本，不让网页停在坏的状态
  if [[ -n $PREV && -d $PREV ]]; then echo "回到上一个版本 $PREV" >&2; start "$PREV" || true; fi
  exit 1
fi

# 只保留最近 5 个版本
ls -1d "$BASE"/releases/*/ | sort | head -n -5 | xargs -r rm -rf
echo "$NAME 已发布：$(cat "$REL/dist-h5/version.json" 2>/dev/null)"
docker ps --filter "name=$NAME"

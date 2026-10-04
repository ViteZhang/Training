#!/usr/bin/env bash
# 将 peetraining-server / peetraining-web 子目录（含历史）同步到云效 Codeup 的独立仓库。
# 需要环境变量：CODEUP_USERNAME、CODEUP_TOKEN、CODEUP_ORG_URL（如 https://codeup.aliyun.com/<组织ID>）
# 用法：scripts/sync-to-codeup.sh [源分支，默认 HEAD] [目标分支，默认 master]
set -euo pipefail

: "${CODEUP_USERNAME:?未设置 CODEUP_USERNAME}"
: "${CODEUP_TOKEN:?未设置 CODEUP_TOKEN}"
: "${CODEUP_ORG_URL:?未设置 CODEUP_ORG_URL}"

SRC_REF="${1:-HEAD}"
DEST_BRANCH="${2:-master}"
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

# 通过临时 credential helper 传递凭据，避免令牌出现在命令行或 .git/config 中
export GIT_ASKPASS="$(mktemp)"
trap 'rm -f "$GIT_ASKPASS"' EXIT
cat > "$GIT_ASKPASS" <<'ASK'
#!/bin/sh
case "$1" in
  Username*) printf '%s' "$CODEUP_USERNAME" ;;
  *) printf '%s' "$CODEUP_TOKEN" ;;
esac
ASK
chmod +x "$GIT_ASKPASS"

for repo in peetraining-server peetraining-web; do
  url="${CODEUP_ORG_URL%/}/${repo}.git"
  echo "==> 拆分 ${repo}"
  sha="$(git subtree split --prefix="$repo" "$SRC_REF")"
  echo "==> 推送 ${sha} 到 ${url} (${DEST_BRANCH})"
  git -c credential.helper= push "$url" "${sha}:refs/heads/${DEST_BRANCH}"
done
echo "完成"

#!/usr/bin/env bash
# 三端同步：GitHub 本仓库（peetraining-server/、peetraining-web/ 两个目录）⇄ 云效两个仓库。
#
# 两个目录与云效仓库用 git subtree 关联（历史已接上），所以同步都是普通的合并与快进推送，不需要强推。
#
#   scripts/sync-codeup.sh status          看两边是否一致、谁领先
#   scripts/sync-codeup.sh pull            把云效的新提交合并进当前分支（先做这一步再开发）
#   scripts/sync-codeup.sh push server|web 把当前分支的目录推到云效 main（只快进，不强推）
#
# 注意：推到云效 peetraining-server 的 main 会触发流水线发布到生产；推送前要确认，
#       有新迁移时先按 runbook 在生产库执行迁移。
#
# 凭证从环境变量读取，不写进仓库与命令行：
#   CODEUP_ORG_URL   如 https://codeup.aliyun.com/<组织 ID>
#   CODEUP_USERNAME  云效 HTTPS 克隆账号
#   CODEUP_TOKEN     云效个人访问令牌（代码仓库读写）
set -euo pipefail

: "${CODEUP_ORG_URL:?需要设置 CODEUP_ORG_URL}"
: "${CODEUP_USERNAME:?需要设置 CODEUP_USERNAME}"
: "${CODEUP_TOKEN:?需要设置 CODEUP_TOKEN}"

cd "$(git rev-parse --show-toplevel)"
export GIT_TERMINAL_PROMPT=0
# 凭证助手在运行时读环境变量，令牌不会出现在命令行、URL 或 git 配置里。
HELPER='!f(){ test "$1" = get && { echo username="$CODEUP_USERNAME"; echo password="$CODEUP_TOKEN"; }; }; f'
g() { git -c credential.helper= -c credential.helper="$HELPER" "$@"; }

url_of() { echo "${CODEUP_ORG_URL%/}/peetraining-$1.git"; }
prefix_of() { echo "peetraining-$1"; }

fetch() { g fetch -q "$(url_of "$1")" "main:refs/remotes/codeup-$1/main"; }

status() {
  local side=$1 prefix remote split
  prefix=$(prefix_of "$side")
  fetch "$side"
  remote=$(git rev-parse "refs/remotes/codeup-$side/main")
  split=$(git subtree split -q --prefix="$prefix" HEAD)
  if [ "$split" = "$remote" ]; then
    echo "$side：一致（云效 main ${remote:0:7}）"
  elif git merge-base --is-ancestor "$remote" "$split"; then
    echo "$side：当前分支领先云效 $(git rev-list --count "$remote..$split") 个提交，可以 push"
  elif git merge-base --is-ancestor "$split" "$remote"; then
    echo "$side：云效领先 $(git rev-list --count "$split..$remote") 个提交，先 pull"
  else
    echo "$side：两边都有新提交，先 pull 合并，再 push"
  fi
}

pull() {
  local side=$1 prefix
  prefix=$(prefix_of "$side")
  fetch "$side"
  if git merge-base --is-ancestor "refs/remotes/codeup-$side/main" "$(git subtree split -q --prefix="$prefix" HEAD)"; then
    echo "$side：云效没有新提交"
    return
  fi
  git subtree merge -q --prefix="$prefix" "refs/remotes/codeup-$side/main" \
    -m "同步云效 peetraining-$side 到 $prefix/（云效 main $(git rev-parse --short "refs/remotes/codeup-$side/main")）"
  echo "$side：已合并云效的新提交"
}

push() {
  local side=$1 prefix remote split
  prefix=$(prefix_of "$side")
  fetch "$side"
  remote=$(git rev-parse "refs/remotes/codeup-$side/main")
  split=$(git subtree split -q --prefix="$prefix" HEAD)
  if [ "$split" = "$remote" ]; then
    echo "$side：云效已是最新，不需要推送"
    return
  fi
  if ! git merge-base --is-ancestor "$remote" "$split"; then
    echo "$side：云效有当前分支没有的提交，先执行 pull，不要强推" >&2
    exit 1
  fi
  echo "$side：将推送到云效 main 的提交："
  git log --oneline "$remote..$split"
  g push "$(url_of "$side")" "$split:refs/heads/main"
}

case "${1:-}" in
  status) status server; status web ;;
  pull) pull server; pull web ;;
  push)
    case "${2:-}" in
      server | web) push "$2" ;;
      *) echo "用法：$0 push server|web" >&2; exit 2 ;;
    esac
    ;;
  *) sed -n '2,17p' "$0"; exit 2 ;;
esac

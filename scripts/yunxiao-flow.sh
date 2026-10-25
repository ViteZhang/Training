#!/usr/bin/env bash
# 云效流水线「peetraining-server上线」：查看、运行、等待结果。运行会把云效 main 发布到生产。
#
#   scripts/yunxiao-flow.sh runs           最近 5 次运行
#   scripts/yunxiao-flow.sh run            运行一次（发布生产），打印运行编号
#   scripts/yunxiao-flow.sh status <编号>   某次运行的状态与各节点结果
#   scripts/yunxiao-flow.sh wait <编号>     等到运行结束（最多 20 分钟），成功返回 0
#   scripts/yunxiao-flow.sh pipeline       流水线配置（看触发设置）
#
# 令牌从环境变量 CODEUP_TOKEN 读取（云效个人访问令牌），经标准输入交给 curl，不出现在命令行里。
# 组织 ID 取自 CODEUP_ORG_URL；流水线 ID 可用 YUNXIAO_PIPELINE_ID 覆盖。
set -euo pipefail

: "${CODEUP_ORG_URL:?需要设置 CODEUP_ORG_URL}"
: "${CODEUP_TOKEN:?需要设置 CODEUP_TOKEN}"
ORG=$(grep -oE '[0-9a-f]{24}' <<<"$CODEUP_ORG_URL" | head -1)
PIPELINE=${YUNXIAO_PIPELINE_ID:-5307719}
BASE="https://openapi-rdc.aliyuncs.com/oapi/v1/flow/organizations/$ORG/pipelines/$PIPELINE"

api() { # api <方法> <路径> [请求体]
  local method=$1 path=$2 body=${3:-}
  local args=(-sS -X "$method" -K - -w '\n%{http_code}')
  [[ -n $body ]] && args+=(-H 'Content-Type: application/json' -d "$body")
  local out code
  out=$(printf 'header = "x-yunxiao-token: %s"\n' "$CODEUP_TOKEN" | curl "${args[@]}" "$BASE$path")
  code=${out##*$'\n'}
  out=${out%$'\n'*}
  if [[ $code != 2* ]]; then
    echo "云效接口返回 HTTP $code：$out" >&2
    return 1
  fi
  printf '%s\n' "$out"
}

# fmt <run|runs|status-only>：把接口返回的 JSON 转成一行行可读的结果。
fmt() {
  python3 -c "$FMT_PY" "$1"
}
FMT_PY=$(cat <<'PY'
import json, sys, datetime
tz = datetime.timezone(datetime.timedelta(hours=8))
def t(x):
    return datetime.datetime.fromtimestamp(x / 1000, tz).strftime("%m-%d %H:%M:%S") if x else "-"
def line(r):
    return "#%s %s 开始 %s 结束 %s" % (r.get("pipelineRunId"), r.get("status"), t(r.get("startTime")), t(r.get("endTime")))
d = json.load(sys.stdin)
mode = sys.argv[1]
if mode == "runs":
    for r in d:
        print(line(r))
elif mode == "status-only":
    print(d.get("status"))
else:
    print(line(d))
    for st in d.get("stages", []):
        jobs = st.get("stageInfo", {}).get("jobs", [])
        print("  %s：%s" % (st.get("name", ""), "，".join("%s %s" % (j.get("name"), j.get("status")) for j in jobs)))
PY
)

case ${1:-} in
  runs)
    api GET "/runs?perPage=5" | fmt runs
    ;;
  run)
    api POST "/runs" '{}'
    ;;
  status)
    api GET "/runs/${2:?需要运行编号}" | fmt run
    ;;
  wait)
    id=${2:?需要运行编号}
    for _ in $(seq 1 80); do
      s=$(api GET "/runs/$id" | fmt status-only)
      case $s in
        SUCCESS) api GET "/runs/$id" | fmt run; exit 0 ;;
        FAIL|FAILED|CANCELED|CANCELLED) api GET "/runs/$id" | fmt run; exit 1 ;;
      esac
      sleep 15
    done
    echo "等待超时，运行 #$id 状态：$s" >&2
    exit 1
    ;;
  pipeline)
    api GET ""
    ;;
  *)
    sed -n '2,10p' "$0"
    exit 2
    ;;
esac

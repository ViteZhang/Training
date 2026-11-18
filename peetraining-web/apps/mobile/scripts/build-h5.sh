#!/usr/bin/env bash
# 构建 H5 版（网页版）：把 App 同一份代码导出成静态网页，输出到 apps/mobile/dist-h5。
# 只生成网页文件，不改动、不触发 App（安卓 / iOS）的打包与热更新。
#
#   scripts/build-h5.sh                 按生产配置构建，部署路径 /h5
#   WEB_BASE_PATH=/m scripts/build-h5.sh   换部署路径
#   API_BASE_URL=http://127.0.0.1:8080/api/v1 scripts/build-h5.sh   本地联调
#
# 发布走云效流水线「peetraining-h5上线」，见 docs/h5.md。
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

export WEB_BASE_PATH="${WEB_BASE_PATH:-/h5}"
export APP_VARIANT="${APP_VARIANT:-production}"
OUT="${H5_OUT_DIR:-dist-h5}"

if [[ $WEB_BASE_PATH != /* || $WEB_BASE_PATH == */ ]]; then
  echo "WEB_BASE_PATH 要以 / 开头、不以 / 结尾，例如 /h5" >&2
  exit 1
fi

rm -rf "$OUT"
npx expo export --platform web --output-dir "$OUT"

# 页面模板里的 %WEB_BASE% 换成部署路径（主屏幕图标、清单文件的地址）
sed -i.bak "s#%WEB_BASE%#${WEB_BASE_PATH}#g" "$OUT/index.html" && rm -f "$OUT/index.html.bak"

# 版本标记：部署后访问 <路径>/version.json 可以确认线上是哪一次构建
printf '{"version":"%s","built_at":"%s","commit":"%s"}\n' \
  "$(grep -oE "version: '[0-9.]+'" app.config.ts | grep -oE '[0-9.]+')" \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  "$(git rev-parse --short HEAD 2>/dev/null || echo unknown)" > "$OUT/version.json"

echo "H5 构建完成：$(cd "$OUT" && pwd)（部署路径 ${WEB_BASE_PATH}，接口 ${API_BASE_URL:-https://training.dreamerlab.cn/api/v1}）"

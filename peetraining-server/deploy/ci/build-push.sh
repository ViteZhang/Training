#!/usr/bin/env bash
# 云效流水线的脚本替代方案：用云效已安装的 Go 编译产物后构建并推送到 ACR。
# 需要的流水线变量：
#   ACR_REGISTRY   ACR 实例地址，如 crpi-xxx.cn-beijing.personal.cr.aliyuncs.com
#   ACR_NAMESPACE  命名空间
#   ACR_USERNAME / ACR_PASSWORD（密码设为私密变量）
#   FONT_IMAGE / RUNTIME_IMAGE（必填）ACR 里的 PDF 字体资源、distroless 运行镜像
# 镜像固定使用 latest tag，流水线不需要维护 VERSION 变量。
set -euo pipefail
for name in ACR_REGISTRY ACR_NAMESPACE ACR_USERNAME ACR_PASSWORD FONT_IMAGE RUNTIME_IMAGE; do
  if [[ -z "${!name:-}" ]]; then
    printf '缺少云效流水线变量：%s\n' "$name" >&2
    exit 2
  fi
done

export GOPROXY=${GOPROXY:-https://goproxy.cn,direct}
export GOTOOLCHAIN=local
case "$(go env GOVERSION)" in
  go1.25.*) ;;
  *) printf '构建机不是 Go 1.25：%s\n' "$(go env GOVERSION)" >&2; exit 2 ;;
esac
export GOOS=linux GOARCH=amd64 CGO_ENABLED=0

IMAGE="${ACR_REGISTRY}/${ACR_NAMESPACE}/peetraining-server"

echo "${ACR_PASSWORD}" | docker login --username "${ACR_USERNAME}" --password-stdin "${ACR_REGISTRY}"
make build

docker build \
  --build-arg "FONT_IMAGE=${FONT_IMAGE}" \
  --build-arg "RUNTIME_IMAGE=${RUNTIME_IMAGE}" \
  -t "${IMAGE}:latest" .
docker push "${IMAGE}:latest"
echo "已推送 ${IMAGE}:latest"

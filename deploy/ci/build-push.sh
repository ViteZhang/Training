#!/usr/bin/env bash
# 云效流水线的脚本替代方案：用 Go 编译产物后构建并推送到 ACR。
# 需要的流水线变量：
#   ACR_REGISTRY   ACR 实例地址，如 crpi-xxx.cn-beijing.personal.cr.aliyuncs.com
#   ACR_NAMESPACE  命名空间
#   ACR_USERNAME / ACR_PASSWORD（密码设为私密变量）
#   FONT_IMAGE / RUNTIME_IMAGE（必填）ACR 里的 PDF 字体资源、distroless 运行镜像
set -euo pipefail
for name in ACR_REGISTRY ACR_NAMESPACE ACR_USERNAME ACR_PASSWORD FONT_IMAGE RUNTIME_IMAGE; do
  if [[ -z "${!name:-}" ]]; then
    printf '缺少云效流水线变量：%s\n' "$name" >&2
    exit 2
  fi
done

VERSION=${VERSION:-$(git describe --tags --always)}
if [[ ! "$VERSION" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]]; then
  printf 'VERSION 不是合法的 Docker 镜像标签：%s\n' "$VERSION" >&2
  exit 2
fi

IMAGE="${ACR_REGISTRY}/${ACR_NAMESPACE}/peetraining-server"

echo "${ACR_PASSWORD}" | docker login --username "${ACR_USERNAME}" --password-stdin "${ACR_REGISTRY}"
make build VERSION="${VERSION}"
printf 'VERSION=%s\n' "${VERSION}" > .release-version

docker build \
  --build-arg "FONT_IMAGE=${FONT_IMAGE}" \
  --build-arg "RUNTIME_IMAGE=${RUNTIME_IMAGE}" \
  -t "${IMAGE}:${VERSION}" .
docker push "${IMAGE}:${VERSION}"
echo "已推送 ${IMAGE}:${VERSION}"

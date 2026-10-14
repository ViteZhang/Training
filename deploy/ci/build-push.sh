#!/usr/bin/env bash
# 云效流水线「构建镜像」阶段：构建并推送到容器镜像服务 ACR，版本号取 git 标签或提交号。
# 需要的流水线变量（在云效里配置为私密变量，不进仓库）：
#   ACR_REGISTRY   如 registry.cn-beijing.aliyuncs.com
#   ACR_NAMESPACE  命名空间
#   ACR_USERNAME / ACR_PASSWORD
#   GO_IMAGE / RUNTIME_IMAGE（可选）ACR 里同步的基础镜像，国内拉不到 docker.io、gcr.io 时设置
set -euo pipefail
for name in ACR_REGISTRY ACR_NAMESPACE ACR_USERNAME ACR_PASSWORD; do
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
args=(--build-arg "VERSION=${VERSION}")
if [[ -n "${GO_IMAGE:-}" ]]; then args+=(--build-arg "GO_IMAGE=${GO_IMAGE}"); fi
if [[ -n "${RUNTIME_IMAGE:-}" ]]; then args+=(--build-arg "RUNTIME_IMAGE=${RUNTIME_IMAGE}"); fi
docker build "${args[@]}" -t "${IMAGE}:${VERSION}" .
docker push "${IMAGE}:${VERSION}"
printf 'VERSION=%s\n' "${VERSION}" > .release-version
echo "已推送 ${IMAGE}:${VERSION}"

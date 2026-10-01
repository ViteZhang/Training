#!/usr/bin/env bash
# 云效流水线「构建镜像」阶段：构建并推送到容器镜像服务 ACR，版本号取 git 标签或提交号。
# 需要的流水线变量（在云效里配置为私密变量，不进仓库）：
#   ACR_REGISTRY   如 registry.cn-beijing.aliyuncs.com
#   ACR_NAMESPACE  命名空间
#   ACR_USERNAME / ACR_PASSWORD
#   GO_IMAGE / RUNTIME_IMAGE（可选）ACR 里同步的基础镜像，国内拉不到 docker.io、gcr.io 时设置
set -euo pipefail
VERSION=${VERSION:-$(git describe --tags --always)}
IMAGE="${ACR_REGISTRY}/${ACR_NAMESPACE}/peetraining-server"

echo "${ACR_PASSWORD}" | docker login --username "${ACR_USERNAME}" --password-stdin "${ACR_REGISTRY}"
args=(--build-arg "VERSION=${VERSION}")
[ -n "${GO_IMAGE:-}" ] && args+=(--build-arg "GO_IMAGE=${GO_IMAGE}")
[ -n "${RUNTIME_IMAGE:-}" ] && args+=(--build-arg "RUNTIME_IMAGE=${RUNTIME_IMAGE}")
docker build "${args[@]}" -t "${IMAGE}:${VERSION}" .
docker push "${IMAGE}:${VERSION}"
echo "VERSION=${VERSION}" > .release-version
echo "已推送 ${IMAGE}:${VERSION}"

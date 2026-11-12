#!/usr/bin/env bash
# 本地构建镜像的快速脚本，避免在命令行反复输入构建参数。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
IMAGE_TAG="${SUB2API_IMAGE:-sub2api-custom:v0.2.18}"
RUNTIME_VERSION="${VERSION:-$(tr -d '\r\n' < "${REPO_ROOT}/backend/cmd/server/VERSION")}"
COMMIT_SHA="${COMMIT:-$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo docker)}"
BUILD_DATE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
BUILD_CONTEXT="${SUB2API_BUILD_CONTEXT:-${REPO_ROOT}}"
DOCKERFILE_PATH="${SUB2API_DOCKERFILE:-${BUILD_CONTEXT}/Dockerfile}"

if [ ! -f "${BUILD_CONTEXT}/Dockerfile" ] || [ ! -f "${BUILD_CONTEXT}/deploy/docker-entrypoint.sh" ]; then
    echo "build context is missing required Dockerfile/deploy/docker-entrypoint.sh: ${BUILD_CONTEXT}" >&2
    exit 1
fi

echo "Building ${IMAGE_TAG} (version=${RUNTIME_VERSION}, commit=${COMMIT_SHA})"
docker build -t "${IMAGE_TAG}" \
    --build-arg VERSION="${RUNTIME_VERSION}" \
    --build-arg COMMIT="${COMMIT_SHA}" \
    --build-arg DATE="${BUILD_DATE}" \
    --build-arg GOPROXY=https://goproxy.cn,direct \
    --build-arg GOSUMDB=sum.golang.google.cn \
    -f "${DOCKERFILE_PATH}" \
    "${BUILD_CONTEXT}"

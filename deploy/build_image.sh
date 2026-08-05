#!/usr/bin/env bash
# 本地构建镜像的快速脚本，避免在命令行反复输入构建参数。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
IMAGE_TAG="${SUB2API_IMAGE:-sub2api-custom:v0.1.171}"
RUNTIME_VERSION="${VERSION:-$(tr -d '\r\n' < "${REPO_ROOT}/backend/cmd/server/VERSION")}"
COMMIT_SHA="${COMMIT:-$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo docker)}"

echo "Building ${IMAGE_TAG} (version=${RUNTIME_VERSION}, commit=${COMMIT_SHA})"
docker build -t "${IMAGE_TAG}" \
    --build-arg VERSION="${RUNTIME_VERSION}" \
    --build-arg COMMIT="${COMMIT_SHA}" \
    --build-arg GOPROXY=https://goproxy.cn,direct \
    --build-arg GOSUMDB=sum.golang.google.cn \
    -f "${REPO_ROOT}/Dockerfile" \
    "${REPO_ROOT}"

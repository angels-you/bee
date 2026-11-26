#!/usr/bin/env bash
# push.sh: builds linux/amd64 and linux/arm64 and pushes the version tag
# and latest to Docker Hub. Only the owner publishes.
#   cmd/push.sh <version>
set -euo pipefail
cd "$(dirname "$0")/.."

IMAGE=angelsyou/bee
VERSION=${1:?usage: cmd/push.sh <version>}

# the default docker driver builds one platform only: a docker-container
# builder is made once and used for the multi-platform build
BUILDER=bee
if ! docker buildx inspect "$BUILDER" >/dev/null 2>&1; then
  docker buildx create --name "$BUILDER" --driver docker-container
fi

docker buildx build -f cmd/Dockerfile \
  --builder "$BUILDER" \
  --platform linux/amd64,linux/arm64 \
  --build-arg VERSION="$VERSION" \
  -t "$IMAGE:$VERSION" -t "$IMAGE:latest" \
  --push .

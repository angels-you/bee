#!/usr/bin/env bash
# push.sh: builds linux/amd64 and linux/arm64 and pushes the version tag
# and latest to Docker Hub. Only the owner publishes.
#   cmd/push.sh <version>
set -euo pipefail
cd "$(dirname "$0")/.."

IMAGE=angelsyou/bee
VERSION=${1:?usage: cmd/push.sh <version>}

docker buildx build -f cmd/Dockerfile \
  --platform linux/amd64,linux/arm64 \
  --build-arg VERSION="$VERSION" \
  -t "$IMAGE:$VERSION" -t "$IMAGE:latest" \
  --push .

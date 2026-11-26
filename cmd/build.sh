#!/usr/bin/env bash
# build.sh: builds the image for the local platform and loads it into docker.
#   cmd/build.sh [version]     default version: the short git commit
set -euo pipefail
cd "$(dirname "$0")/.."

IMAGE=angelsyou/bee
VERSION=${1:-$(git rev-parse --short HEAD 2>/dev/null || echo dev)}

docker build -f cmd/Dockerfile --build-arg VERSION="$VERSION" -t "$IMAGE:$VERSION" -t "$IMAGE:latest" .
echo "built $IMAGE:$VERSION"

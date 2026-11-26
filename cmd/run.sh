#!/usr/bin/env bash
# run.sh: runs the local image with cmd/.env and a local storage folder.
#   cmd/run.sh [out dir]     default: ./out
set -euo pipefail
cd "$(dirname "$0")/.."

IMAGE=angelsyou/bee
OUT=${1:-$PWD/out}
mkdir -p "$OUT"

docker run --rm --name bee \
  --env-file cmd/.env \
  -e BEE_STORAGE=local -e BEE_LOCAL_DIR=/data/out \
  -v "$OUT":/data/out \
  -p 8080:8080 -p 8081:8081 \
  "$IMAGE:latest"

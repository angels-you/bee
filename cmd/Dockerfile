# syntax=docker/dockerfile:1
# bee: the static bee binary plus ffmpeg, ffprobe, fpcalc, vips and
# vipsheader with only their libraries, on scratch.
# Build from the repository root:
#   docker build -f cmd/Dockerfile -t angelsyou/bee .

# --- bee: a static binary -------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.26.3-alpine3.23 AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

# modules first: cached while only the code changes
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# the binary
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/bee ./cmd

# --- tools: alpine packages, gathered into /rootfs ------------------------------
FROM alpine:3.23.8 AS tools

# ffmpeg (x264, x265, svt-av1, aac, webp, avif), fpcalc (chromaprint),
# vips with heif (heic, avif), jpeg, png, webp, tiff, gif
RUN apk add --no-cache ffmpeg chromaprint vips-tools vips-heif ca-certificates

# every binary with the libraries ldd names (the loader included, so each
# architecture finds its own paths); the vips and libheif modules are
# loaded with dlopen, ldd of the binaries does not show them: they are
# copied with their own libraries
RUN set -eu; \
    mkdir -p /rootfs/etc/ssl/certs /rootfs/tmp/bee /rootfs/data/out; \
    bins="/usr/bin/ffmpeg /usr/bin/ffprobe /usr/bin/fpcalc /usr/bin/vips /usr/bin/vipsheader"; \
    mods="$(find /usr/lib -path '*vips-modules*' -name '*.so'; find /usr/lib -path '*libheif*plugins*' -name '*.so')"; \
    for f in $bins $mods; do \
      cp -a --parents "$f" /rootfs; \
      ldd "$f" | awk '/=>/ {print $3} !/=>/ && /^\t\// {print $1}' | while read lib; do \
        [ -e "$lib" ] && cp -aL --parents "$lib" /rootfs; \
      done; \
    done; \
    cp /etc/ssl/certs/ca-certificates.crt /rootfs/etc/ssl/certs/; \
    echo "bee:x:10001:10001:bee:/nonexistent:/sbin/nologin" > /rootfs/etc/passwd; \
    echo "bee:x:10001:" > /rootfs/etc/group; \
    chown 10001:10001 /rootfs/tmp/bee /rootfs/data/out; \
    chmod 1777 /rootfs/tmp

# --- run: scratch ---------------------------------------------------------------
FROM scratch
COPY --from=tools /rootfs /
COPY --from=build /out/bee /usr/local/bin/bee

# the version in the start log
ARG VERSION=dev
ENV BEE_VERSION=$VERSION PATH=/usr/local/bin:/usr/bin

# who made the image and under which license
LABEL org.opencontainers.image.title="bee" \
      org.opencontainers.image.description="Media worker: audio, video and images to local or S3 storage" \
      org.opencontainers.image.vendor="Music Angels" \
      org.opencontainers.image.authors="Music Angels <dev@angels.you>" \
      org.opencontainers.image.url="https://angels.you" \
      org.opencontainers.image.source="https://github.com/angels-you/bee" \
      org.opencontainers.image.licenses="MIT"

# the defaults of every BEE_* key live in the code (internal/config):
# the image sets none of them
USER 10001:10001
VOLUME ["/data/out"]
EXPOSE 8080 8081 9090
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 CMD ["bee", "health"]
ENTRYPOINT ["bee"]

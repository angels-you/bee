# bee: one image with every tool the worker needs.
# Build from the repository root:
#   docker build -f cmd/Dockerfile -t angelsyou/bee .

# --- build --------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.26-trixie AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

# modules first: cached while only the code changes
COPY go.mod go.sum ./
RUN go mod download

# the binary, static
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/bee ./cmd

# --- run ----------------------------------------------------------------------
FROM debian:trixie-slim

# ffmpeg and ffprobe, fpcalc (chromaprint), vips with jpeg, png, webp,
# avif, heic, tiff and gif
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates ffmpeg libchromaprint-tools libvips-tools libheif-plugin-libde265 libheif-plugin-aomenc libheif-plugin-dav1d \
 && rm -rf /var/lib/apt/lists/*

# a user without rights, its temp dir and the default local storage
RUN useradd --system --uid 10001 --home-dir /nonexistent --shell /usr/sbin/nologin bee \
 && mkdir -p /tmp/bee /data/out \
 && chown bee:bee /tmp/bee /data/out

COPY --from=build /out/bee /usr/local/bin/bee

# the version in the start log
ARG VERSION=dev
ENV BEE_VERSION=$VERSION

# the defaults of every BEE_* key live in the code (internal/config):
# the image sets none of them
USER bee
VOLUME ["/data/out"]
EXPOSE 8080 8081 9090
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 CMD ["bee", "health"]
ENTRYPOINT ["bee"]

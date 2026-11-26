# Development

How the repository is laid out, how to build and test bee, and how to
regenerate the generated files. To run bee, see
[getting-started.md](getting-started.md).

## Layout

Module: `github.com/angels-you/bee`.

```
cmd/                     the bee binary (main.go), Dockerfile, .env with every key, build.sh, run.sh, push.sh
internal/                the worker; not importable from outside
  config/                BEE_* environment: defaults, validation, opening the storage
  engine/                job slots, timeouts, downloads, uploads, metrics; one pipeline per type
    audio/               ffmpeg tools for audio: probe, loudness, encode, waveform, fingerprint
    video/               ffmpeg tools for video: plan, filters, encode, HLS playlists, thumbnails, verify
    images/              vips tools for images: probe, render, colors
  servers/               the transports, each calls engine.Do
    http/                POST /v1/<kind>
    grpc/                the Bee service of pkg/proto
    nats/                JetStream consumer, results stream
    health/              /health, /ready, /metrics
  encrypt/               AES-128 HLS segment encryption and EXT-X-KEY playlists
  testmedia/             test sources and the test bench (in-memory storage, nats-server)
pkg/                     public: clients and everything they need
  types/                 the jobs and results
    audio/ video/ images/
    code/                error codes and the Error type
  proto/                 bee.proto, generated gRPC code, transport names (paths, subjects, streams)
  clients/               Go clients
    http/ grpc/ nats/
  storage/               Storage interface and the source downloader
    local/               a folder on disk
    s3/                  one bucket of any S3 provider
      provider/          the provider list (endpoints, regions)
  tools/                 storage keys, urls, job ids
docs/                    this documentation and the logo
```

`internal/` is the service. `pkg/` is what a client imports: job types,
error codes, clients. A client never needs `internal/`.

## Requirements

- **Go** 1.26.3 or newer (from `go.mod`).
- **ffmpeg** and **ffprobe**: audio and video.
- **vips** and **vipsheader** (libvips tools) built with HEIF support
  (heic and avif decoders and the avif encoder). At start bee decodes an
  embedded sample of every input format, so a vips without HEIF does not
  start. On Debian: `libvips-tools libheif-plugin-libde265
  libheif-plugin-aomenc libheif-plugin-dav1d`. On macOS: `brew install vips`.
- **nats-server** in `PATH`: only for the NATS tests (skipped without it).
- **fpcalc** (chromaprint): optional, only for audio fingerprint.
- **openssl**: optional, one encryption test decrypts with it (skipped
  without it).
- **protoc** with `protoc-gen-go` and `protoc-gen-go-grpc`: only to
  regenerate `bee.proto`.
- **docker** (with buildx for multi-platform): only to build the image.

## Build and check

```sh
go build ./...
go vet ./...
go test ./...
```

The binary:

```sh
go build -o bee ./cmd
```

## Tests

`go test` over all packages takes about 1-2 minutes with every tool
installed. What each part needs:

| Packages | Needs | Without it |
|---|---|---|
| `internal/config`, `pkg/tools`, `pkg/types/...`, `pkg/proto`, `pkg/storage`, `pkg/storage/local` | nothing | |
| `pkg/storage/s3` | nothing; the live test needs a bucket | `TestLive` is skipped |
| `internal/encrypt` | openssl for one test | that test is skipped |
| `internal/engine/audio` | ffmpeg | skipped |
| `internal/engine/video` | ffmpeg, ffprobe | skipped |
| `internal/engine/images` | vips, vipsheader with HEIF | **fails** (never skipped) |
| `internal/engine`, `internal/servers/...`, `pkg/clients` | ffmpeg, ffprobe, vips, vipsheader | skipped |
| `internal/servers/nats`, NATS part of `pkg/clients` | also nats-server | skipped |

Test sources are generated on the fly with ffmpeg and vips (plus small
embedded image samples); there are no media fixtures to download. The NATS
tests start their own `nats-server` on a free port.

The live S3 test writes under `test/` in a real bucket and deletes it at
the end. It runs when these are set:

```sh
BEE_TEST_S3_PROVIDER=hetzner BEE_TEST_S3_KEY=... BEE_TEST_S3_SECRET=... BEE_TEST_S3_BUCKET=... \
BEE_TEST_S3_REGION=fsn1 \
go test ./pkg/storage/s3 -run TestLive
```

(`BEE_TEST_S3_ENDPOINT`, `BEE_TEST_S3_REGION`, `BEE_TEST_S3_ACCOUNT` as
the provider needs.)

`TestEnvFile` in `internal/config` loads `cmd/.env` and compares it with the
defaults in the code. When you add a key or change a default, change both
`defaults()` and `cmd/.env`, and update [configuration.md](configuration.md).

## Generated code

### bee.proto

`pkg/proto/bee.proto` is the gRPC service (`Audio`, `Stream`, `Video`,
`Images`, each `Job{json}` to `Result{json}`). After a change, regenerate
`bee.pb.go` and `bee_grpc.pb.go` in `pkg/proto`:

```sh
cd pkg/proto
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       bee.proto
```

The committed files were made with protoc 4.25.3, protoc-gen-go 1.36.11 and
protoc-gen-go-grpc 1.6.2.

### Enum lists

Some enums are kept as plain-text lists next to the Go file generated from
them by the `const` generator:

| List | Generated | What |
|---|---|---|
| `pkg/types/code/code` | `code.go` | error codes with `retryable`, `redeliver` and descriptions |
| `pkg/types/images/fit` | `fit.go` | image fit modes |
| `pkg/types/images/format` | `format.go` | image formats, their mime types and which are outputs |
| `pkg/storage/s3/provider/list` | `list.go` | S3 providers: endpoint templates, regions |

Edit the list, then run the generator on it in its folder (for example
`cd pkg/types/code && const code`). The syntax of a list is documented in
the header comment of each generated file. If you do not have the
generator, change the list and the generated `.go` file together by hand
so they stay the same. A new error code also needs a line in the HTTP and
gRPC status mappings (see [errors.md](errors.md)).

## Docker image

The image is built from the repository root with `cmd/Dockerfile` (about
140 MB): a static Go binary (`CGO_ENABLED=0`, `-trimpath`, `-s -w`) on
`scratch`. An `alpine` stage installs ffmpeg, fpcalc (chromaprint) and vips
with HEIF; the final image gets only these five binaries, the libraries
`ldd` names for them, the vips and libheif modules (loaded with dlopen, so
`ldd` of the binaries misses them) with their libraries, the CA
certificates and a `/etc/passwd` with the user `bee` (uid 10001). There is
no shell: debug in the `tools` stage (`docker build --target tools`). To add
a tool or a library, add the apk package and, if it is loaded with dlopen,
its path to the gathering step. Base images are pinned
(`golang:1.26.3-alpine3.23`, `alpine:3.23.8`).

```sh
cmd/build.sh [version]   # build angelsyou/bee:<version> and :latest for this platform; version defaults to the short git commit
cmd/run.sh [out dir]     # run :latest with cmd/.env, local storage in ./out, ports 8080 and 8081
```

Or directly:

```sh
docker build -f cmd/Dockerfile --build-arg VERSION=dev -t angelsyou/bee:dev .
```

`cmd/push.sh <version>` builds `linux/amd64` and `linux/arm64` and pushes to
Docker Hub. It is for the maintainer only.

`.dockerignore` keeps `docs/`, `*.md` and other non-build files
out of the build context.

## Code style

Keep the style of the code around your change: short comments in plain
English on every type and function, receivers named `a`, small methods,
errors as `*code.Error` with a code from `pkg/types/code`, no new
dependencies without a reason. Every `BEE_*` key has a default in
`internal/config` and a line in `cmd/.env`.

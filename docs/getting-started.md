# Getting started

This page runs one bee with a local output folder, checks that it is healthy and sends a first images job with curl and with Go.

## 1. Run the container

From a checkout of the repository, `cmd/run.sh` runs the local image `angelsyou/bee:latest` with [`cmd/.env`](../cmd/.env) (every key with its default, HTTP on `:8080`, `BEE_TOKEN=change-me`) and a local storage folder:

```sh
cmd/run.sh            # outputs in ./out
cmd/run.sh /srv/out   # outputs in /srv/out
```

Without the repository, the same with `docker run`:

```sh
mkdir -p out
docker run --rm --name bee \
  -e BEE_STORAGE=local -e BEE_LOCAL_DIR=/data/out \
  -e BEE_HTTP_ADDR=:8080 -e BEE_TOKEN=change-me \
  -v "$PWD/out":/data/out \
  -p 8080:8080 -p 8081:8081 \
  angelsyou/bee
```

The image runs as the user `bee` (uid 10001): the mounted folder must be writable by it.

Required keys: `BEE_STORAGE` and the keys of its kind, at least one transport (`BEE_HTTP_ADDR`, `BEE_GRPC_ADDR` or `BEE_NATS_URL`), and `BEE_TOKEN` with HTTP or gRPC. If anything is wrong, bee exits with code 1 and prints every invalid key:

```
bee: the configuration is not valid:
  config: BEE_STORAGE is required: s3 or local
  config: BEE_TOKEN is required with BEE_HTTP_ADDR or BEE_GRPC_ADDR
```

For an S3 bucket instead of a folder, see [storage.md](storage.md); for every key, [configuration.md](configuration.md).

## 2. Check health and readiness

The health server always runs on `BEE_HEALTH_ADDR` (default `:8081`), without a token:

```sh
curl -i localhost:8081/health    # 200 {"ok":true}   the process answers
curl -i localhost:8081/ready     # 200 {"ok":true}   tools, storage and transports are ready
curl localhost:8081/metrics      # Prometheus text format
```

`/ready` answers `503 {"ok":false,"error":"..."}` with the first problem it found. The docker `HEALTHCHECK` of the image runs `bee health`, which asks the health server.

## 3. Send a first job with curl

A job is JSON posted to `POST /v1/<kind>`, where kind is `audio`, `stream`, `video` or `images`. The token goes in the `Authorization` header.

`input` must be an `https` url that resolves to a public address (a presigned GET link works). Plain `http` and private addresses are refused unless `BEE_ALLOW_INSECURE_SOURCE=true` (development only).

```sh
curl -s -X POST localhost:8080/v1/images \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "photo-1",
    "meta": {"user": 7},
    "input": "https://example.com/photo.jpg",
    "tasks": [
      {
        "id": "small",
        "output": "photos/1/small.webp",
        "profile": {"format": "webp", "quality": 80, "max_width": 320}
      },
      {
        "id": "cover",
        "output": "photos/1/cover.jpg",
        "profile": {"format": "jpg", "quality": 85, "fit": "cover", "max_width": 1200, "max_height": 630}
      }
    ]
  }'
```

- `id` is required: a string of 1..128 printable characters, or a whole number. The result returns it exactly as sent.
- `meta` is optional: your own data, at most 16 KB of JSON. bee never reads it and returns it untouched in the result.

The request lasts as long as the job. A done job answers `200` with the result:

```json
{
  "id": "photo-1",
  "meta": {"user": 7},
  "tasks": [
    {"id": "small", "output": "photos/1/small.webp", "format": "webp", "width": 320, "height": 213,
     "size": 9120, "quality": 80, "sha256": "...", "elapsed": 85},
    {"id": "cover", "output": "photos/1/cover.jpg", "format": "jpg", "width": 1200, "height": 630,
     "size": 98411, "quality": 85, "sha256": "...", "elapsed": 140}
  ],
  "elapsed": 612,
  "sha256": "...",
  "source_size": 1843200,
  "source_format": "jpg",
  "source_width": 3000,
  "source_height": 2000
}
```

A failed job answers an HTTP status that matches its error code (for example `400` for `INVALID_JOB`, `422` for `INVALID_AUDIO`, `503` for `INTERNAL_TEMPORARY_ERROR`) and the result with `error` set:

```json
{"id": "photo-1", "meta": {"user": 7}, "elapsed": 210, "error": {"code": "SOURCE_UNAVAILABLE", "message": "...", "retryable": true}}
```

A missing or wrong token answers `401` with `{"error": {"code": "INVALID_JOB", "message": "bad or missing token", "retryable": false}}`. All codes and statuses: [errors.md](errors.md).

The JSON of each job type: [jobs-audio.md](jobs-audio.md), [jobs-video.md](jobs-video.md), [jobs-images.md](jobs-images.md).

## 4. Send the same job from Go

```sh
go get github.com/angels-you/bee
```

```go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/angels-you/bee/pkg/clients/http" // package beehttp
	"github.com/angels-you/bee/pkg/types/code"
	"github.com/angels-you/bee/pkg/types/images"
)

func main() {
	c := &beehttp.Client{URL: "http://localhost:8080", Token: os.Getenv("BEE_TOKEN")}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	res, err := c.GenerateImages(ctx, images.Job{
		ID:    "photo-1",
		Input: "https://example.com/photo.jpg",
		Tasks: []images.Task{{
			ID:      "small",
			Output:  "photos/1/small.webp",
			Profile: images.Profile{Format: images.Webp, Quality: 80, MaxWidth: 320},
		}},
	})
	if err != nil {
		e := code.As(err)
		fmt.Println("failed:", e.Code, e.Message, "retryable:", e.Retryable)
		return
	}

	for _, t := range res.Tasks {
		fmt.Println(t.Output, t.Width, t.Height, t.Size)
	}
}
```

The same program with flags lives in [examples/http](../examples/http/main.go); a gRPC version in [examples/grpc](../examples/grpc/main.go). Over NATS a job is sent and forgotten (`GenerateImages` returns once NATS has it), and a worker of yours reads the results with `SubscribeImages`: [examples/nats](../examples/nats/main.go). Clients in detail: [clients.md](clients.md).

## 5. Where the outputs land

Every `output` in a job is a key in the one storage of this bee, never a bucket or an url:

- `BEE_STORAGE=local`: the file `<BEE_LOCAL_DIR>/<key>`. With the commands above, `photos/1/small.webp` is `./out/photos/1/small.webp` on the host.
- `BEE_STORAGE=s3`: the object `<key>` in `BEE_S3_BUCKET`, under `BEE_S3_PREFIX/` when a prefix is set.

Audio and video publish the entry object (master playlist or MP4) last, so a client that sees it sees a complete output. bee never checks whether keys are taken: every job writes over what lies at its keys, so choosing free keys is up to you.

## Next

- [configuration.md](configuration.md): limits, concurrency, timeouts
- [transports.md](transports.md): gRPC and NATS
- [security.md](security.md): tokens and allowed source hosts

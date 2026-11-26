![bee](https://raw.githubusercontent.com/angels-you/bee/master/docs/beelogo.svg)

# bee

bee is a media worker in one image. You send it a job: a url of the original file and the storage keys of the outputs. bee downloads the original, processes it with ffmpeg or vips, writes the outputs to its storage and answers with a result: what it measured and wrote, or a typed error.

- **Audio**: HLS renditions, waveform, loudness, metadata, optional AES-128 encryption.
- **Video**: HLS stream or one MP4 (h264, hevc, av1), thumbnails, preview.
- **Images**: resize, crop, convert to jpg / png / webp / avif (reads heic, avif, tiff, gif too), colors, thumbhash.

Jobs come over **HTTP**, **gRPC** or **NATS JetStream**. The outputs go to one storage: a local folder or one S3 bucket of any provider.

Source and full documentation: [github.com/angels-you/bee](https://github.com/angels-you/bee)

## Tags

- `latest`: the newest release
- `<version>`, for example `1.0.0`: a fixed release

Platforms: `linux/amd64`, `linux/arm64`.

## Quick start

Local folder storage, HTTP on `:8080`, health on `:8081`:

```sh
mkdir -p out
docker run --rm --name bee \
  -e BEE_STORAGE=local -e BEE_LOCAL_DIR=/data/out \
  -e BEE_HTTP_ADDR=:8080 -e BEE_TOKEN=change-me \
  -v "$PWD/out":/data/out \
  -p 8080:8080 -p 8081:8081 \
  angelsyou/bee
```

One S3 bucket (here Cloudflare R2), HTTP and gRPC:

```sh
docker run --rm --name bee \
  -e BEE_STORAGE=s3 \
  -e BEE_S3_PROVIDER=cloudflare -e BEE_S3_ACCOUNT=<account id> \
  -e BEE_S3_KEY=<key> -e BEE_S3_SECRET=<secret> -e BEE_S3_BUCKET=media \
  -e BEE_HTTP_ADDR=:8080 -e BEE_GRPC_ADDR=:9090 -e BEE_TOKEN=change-me \
  -p 8080:8080 -p 9090:9090 -p 8081:8081 \
  angelsyou/bee
```

Check it:

```sh
curl localhost:8081/health   # {"ok":true}: the process answers
curl localhost:8081/ready    # {"ok":true}: ffmpeg, vips, storage and transports are ready
```

## Send a job

A job is JSON posted to `/v1/audio`, `/v1/stream`, `/v1/video` or `/v1/images`. `input` is an `https` url of the original (a presigned GET link works); `output` is a key in the bee storage, chosen by you.

```sh
curl -s -X POST localhost:8080/v1/images \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "photo-1",
    "input": "https://example.com/photo.jpg",
    "tasks": [
      {"id": "small", "output": "photos/1/small.webp",
       "profile": {"format": "webp", "quality": 80, "max_width": 320}}
    ]
  }'
```

The request lasts as long as the job and answers with the result: sizes, formats, hashes of what was written, or `error` with a code and `retryable`.

Every job needs an `id`: a string of 1..128 printable characters (any language, spaces allowed, no line breaks) or a whole number. The result returns it exactly as sent.

A job may carry `meta`: any JSON object of yours, at most 16 KB. bee never reads it and returns it untouched in the result, on failure too.

Every job that arrives runs and writes over whatever lies at its output keys; choosing free keys is up to you.

Job fields of each type: [audio](https://github.com/angels-you/bee/blob/master/docs/jobs-audio.md), [video](https://github.com/angels-you/bee/blob/master/docs/jobs-video.md), [images](https://github.com/angels-you/bee/blob/master/docs/jobs-images.md).

## Docker Compose with NATS

Several bee containers with the same `BEE_NATS_JOBS` and `BEE_NATS_DONE` share one queue: each job goes to one of them.

```yaml
services:
  nats:
    image: nats
    command: ["-js"]

  bee:
    image: angelsyou/bee
    depends_on: [nats]
    environment:
      BEE_STORAGE: s3
      BEE_S3_PROVIDER: hetzner
      BEE_S3_REGION: hel1
      BEE_S3_KEY: <key>
      BEE_S3_SECRET: <secret>
      BEE_S3_BUCKET: media
      BEE_NATS_URL: nats://nats:4222
      BEE_NATS_JOBS: bee.jobs
      BEE_NATS_DONE: bee.done
    deploy:
      replicas: 3
```

NATS uses two subjects. Publish every job to `bee.jobs`, wrapped with its kind (`audio`, `stream`, `video` or `images`):

```json
{"kind": "images", "job": {"id": 42, "meta": {"user": 7}, "input": "https://example.com/photo.jpg", "tasks": [...]}}
```

Every result comes on `bee.done`, with the kind and the job id:

```json
{"kind": "images", "id": 42, "result": {"id": 42, "meta": {"user": 7}, "tasks": [...], "elapsed": 399}}
```

The job and the result inside are the same JSON as over HTTP; a failed job has `error` in its result. Try it with the `nats` CLI:

```sh
nats sub bee.done
nats pub bee.jobs '{"kind":"images","job":{...}}'
```

Details: [Transports: NATS](https://github.com/angels-you/bee/blob/master/docs/transports.md#nats-jetstream).

## Configuration

Only environment variables. If something is wrong, bee does not start and prints every bad key.

**Storage: pick one**

A local folder:

```sh
BEE_STORAGE=local
BEE_LOCAL_DIR=/data/out      # folder inside the container, mount a volume here
```

Any S3 provider:

```sh
BEE_STORAGE=s3
BEE_S3_PROVIDER=cloudflare   # cloudflare, hetzner, aws, backblaze, minio, ...
BEE_S3_KEY=                  # access key
BEE_S3_SECRET=               # secret key
BEE_S3_BUCKET=               # one bucket per bee
BEE_S3_ACCOUNT=              # optional: account id (cloudflare)
BEE_S3_REGION=               # optional: empty = the provider default
BEE_S3_ENDPOINT=             # optional: empty = the provider default
BEE_S3_PREFIX=               # optional: folder inside the bucket
```

**Transport: at least one**

```sh
BEE_HTTP_ADDR=:8080          # jobs over HTTP
BEE_GRPC_ADDR=:9090          # jobs over gRPC
BEE_TOKEN=                   # required with HTTP or gRPC

BEE_NATS_URL=nats://nats:4222   # jobs over NATS JetStream
BEE_NATS_TOKEN=                 # optional: token of the NATS server
BEE_NATS_JOBS=bee.jobs          # subject of every job
BEE_NATS_DONE=bee.done          # subject of every result
```

**Optional: everything else has a default**

```sh
BEE_HEALTH_ADDR=:8081        # health, ready, metrics
BEE_CONCURRENCY=2            # jobs at once
BEE_SOURCE_HOSTS=            # allow originals only from these hosts; empty = any

BEE_AUDIO_TIMEOUT=5m         # one audio job
BEE_VIDEO_TIMEOUT=10m        # one video job
BEE_IMAGES_TIMEOUT=2m        # one images job

BEE_AUDIO_MAX_SOURCE_MB=500  # largest audio original
BEE_VIDEO_MAX_SOURCE_MB=500  # largest video original
BEE_IMAGES_MAX_SOURCE_MB=50  # largest image original
```

All keys: [cmd/.env](https://github.com/angels-you/bee/blob/master/cmd/.env).

## Ports

```
8080    HTTP jobs               on with BEE_HTTP_ADDR
9090    gRPC jobs               on with BEE_GRPC_ADDR
8081    health, ready, metrics  always on, no token: keep it inside your network
```

## Volumes

```
/data/out    outputs of the local storage, must be writable by uid 10001
/tmp/bee     temp files of running jobs
```

## About the image

- Built `FROM scratch`: the bee binary, ffmpeg, ffprobe, fpcalc, vips and the libraries they need. No shell and no package manager.
- Runs as the user `bee` (uid `10001`), not root.
- `HEALTHCHECK` is built in (`bee health`).
- `docker stop` is graceful: running jobs get `BEE_SHUTDOWN_TIMEOUT` (default `2m`) to finish. Give docker the same time: `docker stop -t 120 bee`.

## Go clients

The Go clients for HTTP, gRPC and NATS and the job types are public packages:

```sh
go get github.com/angels-you/bee
```

Examples: [clients.md](https://github.com/angels-you/bee/blob/master/docs/clients.md).

## Documentation

- [Getting started](https://github.com/angels-you/bee/blob/master/docs/getting-started.md)
- [Configuration](https://github.com/angels-you/bee/blob/master/docs/configuration.md)
- [Storage and S3 providers](https://github.com/angels-you/bee/blob/master/docs/storage.md)
- [Transports: HTTP, gRPC, NATS](https://github.com/angels-you/bee/blob/master/docs/transports.md)
- [Errors](https://github.com/angels-you/bee/blob/master/docs/errors.md)
- [Security](https://github.com/angels-you/bee/blob/master/docs/security.md)

## License

MIT. Copyright (c) 2026 [Music Angels](https://angels.you), dev@angels.you

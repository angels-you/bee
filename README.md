<p align="center"><img src="docs/beelogo.svg" alt="bee" width="400"></p>

# bee

bee is a media worker in one docker image (`angelsyou/bee`). You send it a job: a url of the original file and the storage keys of the outputs. bee downloads the original, processes it with ffmpeg or vips, writes the outputs to its storage and answers with a result: what it measured and wrote, or a typed error.

One bee handles three job types: **audio** (HLS renditions, waveform, loudness, metadata), **video** (HLS stream or one MP4, thumbnails, preview) and **images** (resize, crop, convert to jpg / png / webp / avif, colors, thumbhash). Jobs come over **HTTP**, **gRPC** or **NATS JetStream**; the outputs go to **one** storage: a local folder or one S3 bucket of any provider. The Go clients and job types in `pkg/` are public; bee itself is not a library, you run the container.

## Quick start

Local folder storage, HTTP on :8080, health on :8081:

```sh
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

Required: `BEE_STORAGE` and the keys of its kind, at least one transport (`BEE_HTTP_ADDR`, `BEE_GRPC_ADDR` or `BEE_NATS_URL`), and `BEE_TOKEN` when HTTP or gRPC is on. Everything else has a default; [`cmd/.env`](cmd/.env) lists every key with its default. bee refuses to start and prints every invalid key at once.

Check it:

```sh
curl localhost:8081/health   # {"ok":true}: the process answers
curl localhost:8081/ready    # {"ok":true}: ffmpeg, vips, storage and transports are ready
```

Next: [docs/getting-started.md](docs/getting-started.md).

## Jobs from Go

```sh
go get github.com/angels-you/bee
```

Every client has the same four methods: `GenerateAudio`, `GenerateStream` (HLS), `GenerateVideo` (one MP4) and `GenerateImages`. With HTTP and gRPC a call returns the result when the job is done. With NATS a call only sends the job, and a worker of yours gets the results with `SubscribeAudio`, `SubscribeStream`, `SubscribeVideo` or `SubscribeImages`. Output keys are keys in the bee storage, chosen by you.

**Audio** over HTTP: HLS renditions and a waveform.

```go
import "github.com/angels-you/bee/pkg/clients/http"

c := &beehttp.Client{URL: "http://bee:8080", Token: os.Getenv("BEE_TOKEN")}

res, err := c.GenerateAudio(ctx, audio.Job{
	ID:     "track-1",
	Input:  "https://cdn.example.com/upload/track.mp3",
	Output: "tracks/1/master.m3u8",
	WavePath: "tracks/1/wave.json",
})
if err != nil {
	e := code.As(err) // e.Code, e.Message, e.Retryable
	return e
}
fmt.Println(res.Renditions, res.Loudness)
```

**Video** over gRPC: HLS stream (`GenerateStream`) or one MP4 (`GenerateVideo`).

```go
import "github.com/angels-you/bee/pkg/clients/grpc"

c, err := beegrpc.Dial("bee:9090", false, os.Getenv("BEE_TOKEN"))
if err != nil {
	return err
}
defer c.Close()

res, err := c.GenerateStream(ctx, video.StreamJob{
	ID:       "clip-1",
	Input:    "https://cdn.example.com/upload/clip.mov",
	Output:   "clips/1/index.m3u8",
	Options:  video.VerticalOptions(),
	Profiles: video.VerticalMobile(), // 360p + 720p
	HLS:      video.DefaultHLS(),
})

profile, _ := video.Preset(video.Quality720p)
res, err = c.GenerateVideo(ctx, video.VideoJob{
	ID:      "clip-1-mp4",
	Input:   "https://cdn.example.com/upload/clip.mov",
	Output:  "clips/1/video.mp4",
	Options: video.VerticalOptions(),
	Profile: profile,
})
```

**Images** over NATS: send and forget, the results come to a worker of yours.

```go
package main

import (
	"log"
	"os"
	"os/signal"

	"github.com/angels-you/bee/pkg/clients/nats"
	"github.com/angels-you/bee/pkg/proto"
	"github.com/angels-you/bee/pkg/types/images"
)

func main() {
	// subjects bee.jobs and bee.done
	c, err := beenats.Dial("billing", "nats://nats:4222", "", proto.Names{}) // "billing": the group of your service
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	// the worker: every images result, until Close
	err = c.SubscribeImages(func(res images.Result) {
		if res.Error != nil {
			log.Println(res.ID, res.Meta, res.Error.Code, res.Error.Message)
			return
		}
		for _, t := range res.Tasks {
			log.Println(res.ID, res.Meta, t.Output, t.Width, t.Height, t.Size)
		}
	})
	if err != nil {
		log.Fatal(err)
	}

	// send: returns once NATS has the job
	err = c.GenerateImages(images.Job{
		ID:    42,
		Meta:  map[string]any{"user": 7},
		Input: "https://cdn.example.com/upload/photo.heic",
		Tasks: []images.Task{
			{ID: "small", Output: "photos/42/small.webp", Profile: images.Profile{Format: images.Webp, Quality: 80, MaxWidth: 320}},
			{ID: "large", Output: "photos/42/large.avif", Profile: images.Profile{Format: images.Avif, Quality: 60, MaxWidth: 1600}},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// keep the worker running until ctrl+c
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
}
```

Runnable programs: [examples/http](examples/http/main.go), [examples/grpc](examples/grpc/main.go), [examples/nats](examples/nats/main.go).

Every job needs an `id`: a string of 1..128 printable characters (any language, spaces allowed, no line breaks) or a whole number. In Go: `ID: 123` or `ID: "track-1"`. The result returns the id exactly as sent.

Every job may carry `meta`: your own data (`map[string]any`, at most 16 KB of JSON). bee never reads it and returns it untouched in the result, on failure too.

Every job that arrives runs and writes over whatever lies at its output keys, on every transport (a NATS redelivery runs it again); choosing free keys is up to you.

## Transports

**HTTP**

```sh
BEE_HTTP_ADDR=:8080     # turns HTTP on
BEE_TOKEN=change-me     # header "Authorization: Bearer <token>"
```

Jobs: `POST /v1/audio`, `/v1/stream`, `/v1/video`, `/v1/images`. The answer is the result.

**gRPC**

```sh
BEE_GRPC_ADDR=:9090     # turns gRPC on
BEE_TOKEN=change-me     # metadata "authorization: Bearer <token>"
```

Service `bee.v1.Bee` with the RPC methods `Audio`, `Stream`, `Video`, `Images`. The answer is the result. gRPC carries the job JSON inside `Job{json}` and the result JSON inside `Result{json}`; typed proto messages are postponed.

**NATS JetStream**

```sh
BEE_NATS_URL=nats://nats:4222   # turns NATS on (BEE_TOKEN is not used)
BEE_NATS_TOKEN=                 # optional: token of the NATS server
BEE_NATS_JOBS=bee.jobs          # every job is published here
BEE_NATS_DONE=bee.done          # every result is published here
```

A job is `{"kind":"images","job":{...}}` on `bee.jobs`; its result comes as `{"kind":"images","id":42,"result":{...}}` on `bee.done`.

The job and the result are the same JSON on every transport. Health, readiness and Prometheus metrics are always on `BEE_HEALTH_ADDR` (`:8081`), without a token. Details: [docs/transports.md](docs/transports.md).

## Documentation

- [Getting started](docs/getting-started.md): run, check, send the first job
- [Go clients](docs/clients.md): HTTP, gRPC, NATS, errors, timeouts
- [Configuration](docs/configuration.md): every `BEE_*` key
- [Storage](docs/storage.md): local folder and S3 providers
- [Transports](docs/transports.md): HTTP, gRPC, NATS in detail
- [Audio jobs](docs/jobs-audio.md)
- [Video jobs](docs/jobs-video.md)
- [Image jobs](docs/jobs-images.md)
- [Errors](docs/errors.md): codes, retries, HTTP statuses
- [Security](docs/security.md): tokens, source urls, keys
- [Development](docs/development.md): build, test, docker image

## License

MIT, see [LICENSE](LICENSE).

Copyright (c) 2026 [Music Angels](https://angels.you), dev@angels.you

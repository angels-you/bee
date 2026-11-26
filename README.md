<p align="center"><img src="docs/beelogo.svg" alt="bee" width="200"></p>

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

Every client has the same four methods: `Audio`, `Stream`, `Video`, `Images`. A call returns when the job is done. Output keys are keys in the bee storage, chosen by you.

**Audio**: HLS renditions and a waveform.

```go
c := &http.Client{URL: "http://bee:8080", Token: os.Getenv("BEE_TOKEN")} // pkg/clients/http

res, err := c.Audio(ctx, audio.Job{
	ID:     "track-1",
	Input:  "https://cdn.example.com/upload/track.mp3",
	Output: "tracks/1/master.m3u8",
	Wave:   "tracks/1/wave.json",
})
if err != nil {
	e := code.As(err) // e.Code, e.Message, e.Retryable
	return e
}
fmt.Println(res.Renditions, res.Loudness)
```

**Video**: HLS stream (`Stream`) or one MP4 (`Video`).

```go
c, err := grpc.Dial("bee:9090", false, os.Getenv("BEE_TOKEN")) // pkg/clients/grpc
if err != nil {
	return err
}
defer c.Close()

res, err := c.Stream(ctx, video.Stream{
	ID:       "clip-1",
	Input:    "https://cdn.example.com/upload/clip.mov",
	Output:   "clips/1/index.m3u8",
	Options:  video.VerticalOptions(),
	Profiles: video.VerticalMobile(), // 360p + 720p
	HLS:      video.DefaultHLS(),
})

profile, _ := video.Preset(video.Quality720p)
res, err = c.Video(ctx, video.Video{
	ID:      "clip-1-mp4",
	Input:   "https://cdn.example.com/upload/clip.mov",
	Output:  "clips/1/video.mp4",
	Options: video.VerticalOptions(),
	Profile: profile,
})
```

**Images**: several results from one original.

```go
c, err := nats.Dial("nats://nats:4222", "", proto.Names{}) // pkg/clients/nats
if err != nil {
	return err
}
defer c.Close()

res, err := c.Images(ctx, images.Job{
	ID:    "photo-1",
	Input: "https://cdn.example.com/upload/photo.heic",
	Tasks: []images.Task{
		{ID: "small", Output: "photos/1/small.webp", Profile: images.Profile{Format: images.Webp, Quality: 80, MaxWidth: 320}},
		{ID: "large", Output: "photos/1/large.avif", Profile: images.Profile{Format: images.Avif, Quality: 60, MaxWidth: 1600}},
	},
})
for _, t := range res.Tasks {
	fmt.Println(t.Output, t.Width, t.Height, t.Size)
}
```

Runnable programs: [examples/http](examples/http/main.go), [examples/grpc](examples/grpc/main.go), [examples/nats](examples/nats/main.go).

Job ids are 1..128 characters of `A-Z a-z 0-9 _ -`. Audio and video refuse an output that is already published (`ALREADY_EXISTS`: a new run needs new keys), except a NATS redelivery of the same job, which gets its saved result back. Images overwrite existing keys.

## Transports

| Transport | Turn on with | Auth | Endpoint |
|---|---|---|---|
| HTTP | `BEE_HTTP_ADDR=:8080` | `BEE_TOKEN`, header `Authorization: Bearer <token>` | `POST /v1/audio`, `/v1/stream`, `/v1/video`, `/v1/images` |
| gRPC | `BEE_GRPC_ADDR=:9090` | `BEE_TOKEN`, metadata `authorization: Bearer <token>` | service `bee.v1.Bee`: `Audio`, `Stream`, `Video`, `Images` |
| NATS JetStream | `BEE_NATS_URL=nats://nats:4222` | no `BEE_TOKEN`; optional `BEE_NATS_TOKEN` for the nats server | jobs on `<BEE_NATS_JOBS>.<kind>`, results on `<BEE_NATS_DONE>.<kind>.<id>` (defaults `bee.jobs.new`, `bee.jobs.done`) |

The job and the result are the same JSON on every transport. gRPC carries that JSON inside `Job{json}` / `Result{json}` messages; typed proto messages are postponed. Health, readiness and Prometheus metrics are always on `BEE_HEALTH_ADDR` (`:8081`), without a token. Details: [docs/transports.md](docs/transports.md).

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

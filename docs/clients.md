# Go clients

bee ships three Go clients, one per transport, in the module `github.com/angels-you/bee`:

| Package | Transport | Auth |
|---|---|---|
| `pkg/clients/http` | HTTP/JSON | `BEE_TOKEN` |
| `pkg/clients/grpc` | gRPC | `BEE_TOKEN` |
| `pkg/clients/nats` | NATS JetStream | nats server token, if any (not `BEE_TOKEN`) |

They share one API and the public job types:

```go
Audio(ctx context.Context, j audio.Job) (audio.Result, error)
Stream(ctx context.Context, j video.Stream) (video.Result, error) // HLS
Video(ctx context.Context, j video.Video) (video.Result, error)   // one MP4
Images(ctx context.Context, j images.Job) (images.Result, error)
```

Types live in `pkg/types/audio`, `pkg/types/video`, `pkg/types/images`; errors in `pkg/types/code`. A call returns when the job is done (or failed, or the context ended). The clients are safe for concurrent calls; bee itself runs at most `BEE_CONCURRENCY` jobs at once.

```sh
go get github.com/angels-you/bee
```

## HTTP

```go
import "github.com/angels-you/bee/pkg/clients/http"

type Client struct {
	URL   string        // http://bee:8080
	Token string        // BEE_TOKEN
	HTTP  *http.Client  // nil: http.DefaultClient
}
```

```go
c := &http.Client{URL: "http://bee:8080", Token: os.Getenv("BEE_TOKEN")}
res, err := c.Images(ctx, job)
```

Each call is `POST <URL>/v1/<kind>` with `Authorization: Bearer <Token>`. There is no connection to open or close. One call holds the request open for the whole job, so a custom `HTTP.Timeout` must be longer than the job timeout of bee (`BEE_AUDIO_TIMEOUT`, `BEE_VIDEO_TIMEOUT`, `BEE_IMAGES_TIMEOUT`).

## gRPC

```go
import "github.com/angels-you/bee/pkg/clients/grpc"

func Dial(addr string, tls bool, token string, opts ...grpc.DialOption) (*Client, error)
func (a *Client) Close() error
```

```go
c, err := grpc.Dial("bee:9090", false, os.Getenv("BEE_TOKEN"))
if err != nil {
	return err
}
defer c.Close()

res, err := c.Audio(ctx, job)
```

- `addr` is `host:port`.
- `tls` false: plain connection (a bee in the same network); true: TLS with the system roots (a bee behind a TLS proxy).
- `token` goes into the metadata `authorization: Bearer <token>` of every call.
- `opts` are extra `google.golang.org/grpc` dial options.

One connection is reused by every call. The service is `bee.v1.Bee` ([`pkg/proto/bee.proto`](../pkg/proto/bee.proto)); its messages `Job{json}` and `Result{json}` carry the same JSON as HTTP and NATS. Typed proto messages are postponed. A failed job is a gRPC status with the bee code in the trailers `bee-error-code` and `bee-retryable`; the client turns it back into `*code.Error`.

## NATS

```go
import "github.com/angels-you/bee/pkg/clients/nats"

func Dial(url, token string, names proto.Names, opts ...nats.Option) (*Client, error)
func New(nc *nats.Conn, names proto.Names) (*Client, error)
func (a *Client) Close()
func (a *Client) Result(ctx context.Context, kind proto.Kind, id string, res any) error
```

```go
c, err := nats.Dial("nats://nats:4222", os.Getenv("NATS_TOKEN"), proto.Names{})
if err != nil {
	return err
}
defer c.Close()

res, err := c.Video(ctx, job)
```

- `token` is the auth token of the nats server (empty: none); bee reads the same token from `BEE_NATS_TOKEN`. `BEE_TOKEN` is not used on NATS.
- `names` (`pkg/proto`) are the subject bases of the bees you talk to: `proto.Names{Jobs: ..., Done: ...}` must match their `BEE_NATS_JOBS` and `BEE_NATS_DONE`. The zero value `proto.Names{}` is the defaults `bee.jobs.new` and `bee.jobs.done`.
- `New` uses a connection you already have; `Close` closes only a connection made by `Dial`.

A call publishes the job to `<jobs>.<kind>` (a JetStream work queue shared by every bee with the same bases) and waits for the result on `<done>.<kind>.<id>`. Because the subject contains the id, the client checks the id before sending: 1..128 characters of `A-Z a-z 0-9 _ -`.

The job lives in JetStream, not in the call: it survives a restart of bee and is delivered again on retryable errors (up to `BEE_NATS_MAX_DELIVER` times). If the context ends first, the job still runs and its result stays in the results stream for `BEE_NATS_RESULT_TTL` (default 24h). Read it later with `Result`:

```go
var res video.Result
err := c.Result(ctx, proto.Video, "clip-1", &res) // the latest result of job clip-1
```

`res` is a pointer to `audio.Result`, `video.Result` or `images.Result`; `kind` is `proto.Audio`, `proto.Stream`, `proto.Video` or `proto.Images`.

## Errors

Every method returns the result and an error:

- **Done:** `err == nil`, the result holds what bee measured and wrote.
- **Failed job:** `err` is a `*code.Error` and the result is filled too, with `ID` and `Error` set to the same `*code.Error`. Nothing of a failed job counts, whatever is already in storage.
- **Transport failure** (bee not reachable, connection dropped, unreadable answer, no NATS result before the context ended): `INTERNAL_TEMPORARY_ERROR`, retryable, also in `res.Error`.
- **Bad token:** `INVALID_JOB` (HTTP 401, gRPC `Unauthenticated`).

```go
res, err := c.Stream(ctx, job)
if err != nil {
	e := code.As(err) // *code.Error for any error
	switch {
	case e.Code == code.AlreadyExists:
		// the output keys are taken: send the job with new keys
	case e.Retryable:
		// try again later; SOURCE_URL_EXPIRED needs a new job with a fresh url
	default:
		// the job or the source is wrong: do not retry
	}
	log.Println(e.Code, e.Message)
}
```

`code.Error` is `{Code, Message, Retryable}` in JSON (`code`, `message`, `retryable`). `Retryable` mirrors `Code.Retryable()`, so the client needs no code table. `code.As` returns any error as `*code.Error`: a typed one as is, a context deadline or cancel as `INTERNAL_TEMPORARY_ERROR`. Every code: [errors.md](errors.md).

`ALREADY_EXISTS`: audio and video refuse an output whose entry object (master playlist or MP4) is already published. The exception is a redelivery of the same job on NATS: bee finds the saved result in its ledger and returns it again. Images overwrite existing keys.

## Timeouts and contexts

- bee bounds every job by its type timeout (`BEE_AUDIO_TIMEOUT` 5m, `BEE_VIDEO_TIMEOUT` 10m, `BEE_IMAGES_TIMEOUT` 2m) and the downloads by `BEE_DOWNLOAD_TIMEOUT` (2m). A job cannot change them.
- With HTTP and gRPC the job runs inside the request: give the context a little more than the bee timeout of that type. A context that ends first fails the call with `INTERNAL_TEMPORARY_ERROR`.
- When every slot is busy, bee waits up to `BEE_QUEUE_WAIT` (default 0s: refuse at once) and then answers `INTERNAL_TEMPORARY_ERROR` "all job slots are busy". On HTTP and gRPC retry later. On NATS bee pulls only as many jobs as it has slots; the rest wait in the queue, and a refused one is delivered again.
- With NATS the context only bounds the wait for the result: the job is not cancelled. Use `Result` to pick the result up later.

Runnable examples: [examples/http](../examples/http/main.go), [examples/grpc](../examples/grpc/main.go), [examples/nats](../examples/nats/main.go). Transport details: [transports.md](transports.md).

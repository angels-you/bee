# Go clients

bee ships three Go clients, one per transport, in the module `github.com/angels-you/bee`:

```
pkg/clients/http    package beehttp    HTTP/JSON        auth: BEE_TOKEN
pkg/clients/grpc    package beegrpc    gRPC             auth: BEE_TOKEN
pkg/clients/nats    package beenats    NATS JetStream   auth: token of the nats server, if any
```

Import a client by its path, without an alias; the package name is `beehttp`, `beegrpc` or `beenats`:

```go
import "github.com/angels-you/bee/pkg/clients/nats"

c, err := beenats.Dial(...)
```

```sh
go get github.com/angels-you/bee
```

Types live in `pkg/types/audio`, `pkg/types/video`, `pkg/types/images`; errors in `pkg/types/code`. The job types are `audio.Job`, `video.StreamJob` (HLS), `video.VideoJob` (one MP4) and `images.Job`.

Every client has the same four methods: `GenerateAudio`, `GenerateStream`, `GenerateVideo` and `GenerateImages`, and is safe for concurrent calls. bee itself runs at most `BEE_CONCURRENCY` jobs at once.

Two ways to work:

- **HTTP and gRPC: wait for the result.** A call returns the result when the job is done (or failed, or the context ended).
- **NATS: send and forget.** A call only sends the job and returns once NATS has it. A worker of yours gets the results with `SubscribeAudio`, `SubscribeStream`, `SubscribeVideo` or `SubscribeImages`.

## HTTP

```go
import "github.com/angels-you/bee/pkg/clients/http"

type Client struct {
	URL   string        // http://bee:8080
	Token string        // BEE_TOKEN
	HTTP  *http.Client  // nil: http.DefaultClient
}

func (a *Client) GenerateAudio(ctx context.Context, j audio.Job) (audio.Result, error)
func (a *Client) GenerateStream(ctx context.Context, j video.StreamJob) (video.Result, error)
func (a *Client) GenerateVideo(ctx context.Context, j video.VideoJob) (video.Result, error)
func (a *Client) GenerateImages(ctx context.Context, j images.Job) (images.Result, error)
```

```go
c := &beehttp.Client{URL: "http://bee:8080", Token: os.Getenv("BEE_TOKEN")}
res, err := c.GenerateImages(ctx, job)
```

Each call is `POST <URL>/v1/<kind>` with `Authorization: Bearer <Token>`. There is no connection to open or close. One call holds the request open for the whole job, so a custom `HTTP.Timeout` must be longer than the job timeout of bee (`BEE_AUDIO_TIMEOUT`, `BEE_VIDEO_TIMEOUT`, `BEE_IMAGES_TIMEOUT`).

## gRPC

```go
import "github.com/angels-you/bee/pkg/clients/grpc"

func Dial(addr string, tls bool, token string, opts ...grpc.DialOption) (*Client, error)
func (a *Client) Close() error
// GenerateAudio, GenerateStream, GenerateVideo, GenerateImages:
// the same signatures as the HTTP client
```

```go
c, err := beegrpc.Dial("bee:9090", false, os.Getenv("BEE_TOKEN"))
if err != nil {
	return err
}
defer c.Close()

res, err := c.GenerateAudio(ctx, job)
```

- `addr` is `host:port`.
- `tls` false: plain connection (a bee in the same network); true: TLS with the system roots (a bee behind a TLS proxy).
- `token` goes into the metadata `authorization: Bearer <token>` of every call.
- `opts` are extra `google.golang.org/grpc` dial options.

One connection is reused by every call. The service is `bee.v1.Bee` ([`pkg/proto/bee.proto`](../pkg/proto/bee.proto)) with the RPC methods `Audio`, `Stream`, `Video`, `Images`; its messages `Job{json}` and `Result{json}` carry the same JSON as HTTP and NATS. Typed proto messages are postponed. A failed job is a gRPC status with the bee code in the trailers `bee-error-code` and `bee-retryable`; the client turns it back into `*code.Error`.

## NATS

```go
import "github.com/angels-you/bee/pkg/clients/nats"

func Dial(group, url, token string, names proto.Names, opts ...nats.Option) (*Client, error)
func New(group string, nc *nats.Conn, names proto.Names) (*Client, error)
func (a *Client) Close()

// send a job
func (a *Client) GenerateAudio(j audio.Job) error
func (a *Client) GenerateStream(j video.StreamJob) error
func (a *Client) GenerateVideo(j video.VideoJob) error
func (a *Client) GenerateImages(j images.Job) error

// read the results
func (a *Client) SubscribeAudio(handler func(res audio.Result)) error
func (a *Client) SubscribeStream(handler func(res video.Result)) error
func (a *Client) SubscribeVideo(handler func(res video.Result)) error
func (a *Client) SubscribeImages(handler func(res images.Result)) error
```

- `url` is the nats server, e.g. `nats://nats:4222`.
- `token` is the auth token of the nats server (empty: none); bee reads the same token from `BEE_NATS_TOKEN`. `BEE_TOKEN` is not used on NATS.
- `names` (`pkg/proto`) are the two subjects of the bees you talk to. `proto.Names{Jobs: ..., Done: ...}` must match their `BEE_NATS_JOBS` and `BEE_NATS_DONE`. The zero value `proto.Names{}` is the defaults `bee.jobs` and `bee.done`.
- `New` uses a connection you already have; `Close` stops the subscribers and closes only a connection made by `Dial`.

### Send and worker: a complete program

```go
package main

import (
	"log"
	"os"
	"os/signal"

	"github.com/angels-you/bee/pkg/clients/nats"
	"github.com/angels-you/bee/pkg/proto"
	"github.com/angels-you/bee/pkg/types/audio"
)

func main() {
	c, err := beenats.Dial("billing", "nats://nats:4222", os.Getenv("NATS_TOKEN"), proto.Names{})
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	// the worker: every audio result, until Close
	err = c.SubscribeAudio(func(res audio.Result) {
		// a failed job
		if res.Error != nil {
			log.Println(res.ID, res.Meta, res.Error.Code, res.Error.Message, res.Error.Retryable)
			return
		}

		// a done job
		log.Println(res.ID, res.Meta, res.Elapsed, len(res.Renditions))
	})
	if err != nil {
		log.Fatal(err)
	}

	// send: returns once NATS has the job
	err = c.GenerateAudio(audio.Job{
		ID:     1001,
		Meta:   map[string]any{"album": 7},
		Input:  "https://cdn.example.com/upload/track.mp3",
		PlaylistPath: "tracks/1001/master.m3u8",
		WavePath: "tracks/1001/wave.json",
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

The sender and the worker do not have to be one program: a web service may only send, and another service may only subscribe.

### Send a job

A `Generate` call checks the job first and returns a `*code.Error` (`INVALID_JOB` or `INVALID_PATH`) if it is wrong. Audio and images jobs get the full check; video jobs get a light one (id, input url, output key), bee checks the rest. Then it publishes the job to the jobs subject and returns `nil` once JetStream has accepted it (it waits up to 10 seconds). There is no context: the call never waits for the job itself.

From then on the job lives in NATS, not in your program: it survives a restart of bee and is delivered again on temporary errors (up to `BEE_NATS_MAX_DELIVER` times).

### Read the results

- **`Subscribe...` returns at once.** The handler runs in the background for every result of its kind until `Close`.
- **Failed jobs come to the same handler**, with `res.Error` set.
- **The result is done (acked) after the handler returns**, and does not come again.
- **A panic in the handler** does not crash your program: the result comes again about 5 seconds later.
- **Every kind has its own subscriber.** Subscribe only to the kinds you send; results of other kinds are skipped.

**Groups.** The first argument of `Dial` and `New` is the group: the name of your service that reads the results, for example `"billing"` or `"api"`. It is required and decides who shares the results; letters, digits, `_` and `-` only.

- **Same group: shared.** Several instances of one service with the same group share the results: each result goes to one of them.
- **Other group: a copy.** Another service with another group gets its own copy of every result.

```go
c, err := beenats.Dial("billing", url, token, proto.Names{}) // the billing service gets its own copy of every result
```

NATS remembers what each group has already got, so a restarted worker goes on where it stopped. The durable consumer is named `<done stream>_<GROUP>_<KIND>`, for example `BEE_DONE_BILLING_AUDIO`.

Results stay in NATS for `BEE_NATS_RESULT_TTL` (default `24h`). A worker that starts later, or a new group, still gets every result of that time.

A result can come twice: when a job is delivered again (a lost ack, a bee that died), it runs again and publishes its result again. Make the handler safe to run twice for the same `res.ID`.

## Errors

HTTP and gRPC methods return the result and an error:

- **Done:** `err == nil`, the result holds what bee measured and wrote.
- **Failed job:** `err` is a `*code.Error` and the result is filled too, with `ID` and `Error` set to the same `*code.Error`. Nothing of a failed job counts, whatever is already in storage.
- **Transport failure** (bee not reachable, connection dropped, unreadable answer): `INTERNAL_TEMPORARY_ERROR`, retryable, also in `res.Error`.
- **Bad token:** `INVALID_JOB` (HTTP 401, gRPC `Unauthenticated`).

```go
res, err := c.GenerateStream(ctx, job)
if err != nil {
	e := code.As(err) // *code.Error for any error
	switch {
	case e.Retryable:
		// try again later; SOURCE_URL_EXPIRED needs a new job with a fresh url
	default:
		// the job or the source is wrong: do not retry
	}
	log.Println(e.Code, e.Message)
}
```

On NATS a send returns only send errors: a job that is not valid (`*code.Error`) or a NATS error (no connection, no answer from JetStream). The failure of the job itself comes later to your handler: `res.Error`.

`code.Error` is `{Code, Message, Retryable}` in JSON (`code`, `message`, `retryable`). `Retryable` mirrors `Code.Retryable()`, so the client needs no code table. `code.As` returns any error as `*code.Error`: a typed one as is, a context deadline or cancel as `INTERNAL_TEMPORARY_ERROR`. Every code: [errors.md](errors.md).

Existing keys: bee never checks them. Every job type writes over whatever lies at its output keys; keeping keys apart is up to you. A job sent again always runs again, on every transport; so does a NATS redelivery ([transports.md](transports.md#redelivery)).

## Job id

Every job needs an `id`, on every transport:

- a string of 1..128 printable characters: letters of any language, digits, punctuation and spaces are fine; line breaks and other control characters are not;
- or a whole number: any Go integer, a float without a fraction, a JSON number.

`nil`, `bool`, objects, arrays and the empty string are `INVALID_JOB`. In Go: `ID: 123` or `ID: "track-1"`.

The result returns the id exactly as sent: a number stays a number, a big integer keeps all its digits. A result decoded by the Go clients holds a number as `json.Number`, so print it or call `.String()` instead of a type switch on `int`.

## Meta

Every job and every result has `Meta map[string]any` (JSON `meta`): your own data, for example a user id or a database row. bee never reads it, never logs it, and returns it untouched in the result, on failure too. At most 16 KB of JSON, else `INVALID_JOB`. Numbers in it come back as `json.Number` in Go.

```go
job := images.Job{ID: 42, Meta: map[string]any{"user": 7, "album": "summer"}, ...}
```

## Timeouts and contexts

- bee bounds every job by its type timeout (`BEE_AUDIO_TIMEOUT` 5m, `BEE_VIDEO_TIMEOUT` 10m, `BEE_IMAGES_TIMEOUT` 2m) and the downloads by `BEE_DOWNLOAD_TIMEOUT` (2m). A job cannot change them.
- With HTTP and gRPC the job runs inside the request: give the context a little more than the bee timeout of that type. A context that ends first fails the call with `INTERNAL_TEMPORARY_ERROR`.
- When every slot is busy, bee waits up to `BEE_QUEUE_WAIT` (default 0s: refuse at once) and then answers `INTERNAL_TEMPORARY_ERROR` "all job slots are busy". On HTTP and gRPC retry later. On NATS bee pulls only as many jobs as it has slots; the rest wait in the queue, and a refused one is delivered again.
- With NATS there is no context: a send waits only for JetStream (up to 10 seconds), and the subscribers run until `Close`.

Runnable examples: [examples/http](../examples/http/main.go), [examples/grpc](../examples/grpc/main.go), [examples/nats](../examples/nats/main.go) (`-work` runs the worker). Transport details: [transports.md](transports.md).

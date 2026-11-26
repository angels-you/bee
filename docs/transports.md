# Transports: the wire protocol

Bee accepts the same jobs over HTTP, gRPC and NATS JetStream. Any
combination can be on at once, and all of them share one engine, one set of
job slots (`BEE_CONCURRENCY`) and one storage. This page describes the
protocol for clients that do not use the Go packages in `pkg/clients` (see
[clients.md](clients.md)).

On every transport, a job is the json of its type, and so is the result
(on NATS both are wrapped in an [envelope](#the-message-one-envelope-both-ways)):

| kind | job json | result json | page |
|---|---|---|---|
| `audio` | `audio.Job` | `audio.Result` | [jobs-audio.md](jobs-audio.md) |
| `stream` | `video.StreamJob` | `video.Result` | [jobs-video.md](jobs-video.md) |
| `video` | `video.VideoJob` | `video.Result` | [jobs-video.md](jobs-video.md) |
| `images` | `images.Job` | `images.Result` | [jobs-images.md](jobs-images.md) |

**Job id rule.** On every transport, a job needs an `id`:

- a string of 1..128 printable characters: letters of any language,
  digits, punctuation and spaces are fine (uuid, ulid, `фото 1`); line
  breaks and other control characters are not;
- or a whole number (`42`, or a big integer of any length).

`null`, `true`/`false`, objects, arrays and `""` are `INVALID_JOB`. The
result returns the id exactly as sent: a number stays a number, a big
integer keeps all its digits.

**Meta.** Every job may carry `meta`: a json object of yours (a user id, a
row id, anything). bee never reads it, never logs it, and returns it
untouched in the result, on failure too. At most 16 KB of json, else
`INVALID_JOB`.

**Failures.** A failed job has `"error": {"code", "message", "retryable"}`
in its result. `retryable` tells whether the same work can succeed later.
The full code list is in [errors.md](errors.md).

Configuration keys mentioned here are described in
[configuration.md](configuration.md).

## HTTP

Enabled by `BEE_HTTP_ADDR` (e.g. `:8080`). `BEE_TOKEN` is required.

```
POST /v1/audio
POST /v1/stream
POST /v1/video
POST /v1/images
Authorization: Bearer <BEE_TOKEN>
Content-Type: application/json

<job json>
```

The call is synchronous: the answer comes when the job ends.

- **Answer:** the result json, with `Content-Type: application/json` and
  the status below.
- **Client timeout:** set it to at least the longest job timeout of the
  type. The server allows `max(BEE_*_TIMEOUT) + 30s` to write the answer.
- **Disconnects:** closing the request cancels the job.
- **Body size:** at most 1 MiB.

```sh
curl -sS -X POST http://bee:8080/v1/images \
  -H "Authorization: Bearer $BEE_TOKEN" \
  -H 'Content-Type: application/json' \
  --data @job.json
```

### Status codes

| status | codes |
|---|---|
| 200 | the job is done (no `error`) |
| 400 | `INVALID_JOB`, `INVALID_PATH`, `INVALID_DIMENSIONS`, `INVALID_QUALITY` |
| 401 | missing or wrong token |
| 410 | `SOURCE_URL_EXPIRED` |
| 413 | `SOURCE_TOO_LARGE` |
| 422 | `UNSUPPORTED_CODEC`, `UNSUPPORTED_FORMAT`, `INVALID_AUDIO`, `INVALID_VIDEO`, `INVALID_IMAGE`, `INVALID_SOUND`, `INVALID_WATERMARK`, `DIMENSION_LIMIT_EXCEEDED`, `PIXEL_LIMIT_EXCEEDED`, `DURATION_LIMIT_EXCEEDED`, `NO_RENDITION`, `OUTPUT_TOO_LARGE`, `ENCODE_FAILED`, `VERIFY_FAILED` |
| 424 | `SOURCE_UNAVAILABLE` |
| 502 | `STORAGE_UNAVAILABLE` |
| 503 | `INTERNAL_TEMPORARY_ERROR` (including "all job slots are busy") |
| 504 | `DOWNLOAD_TIMEOUT`, `STORAGE_TIMEOUT` |
| 500 | any other code |

A 401 answer, or an unreadable or oversized body, has only an error and no
id:

```json
{"error": {"code": "INVALID_JOB", "message": "bad or missing token", "retryable": false}}
```

A body that is not a json job gets `400` with `{"id": "<if readable>",
"error": {"code": "INVALID_JOB", …}}`. Any other failure returns the full
result of its type with `error` set.

## gRPC

Enabled by `BEE_GRPC_ADDR` (e.g. `:9090`). `BEE_TOKEN` is required. The
service is defined in `pkg/proto/bee.proto`:

```proto
package bee.v1;

service Bee {
  rpc Audio(Job) returns (Result);
  rpc Stream(Job) returns (Result);
  rpc Video(Job) returns (Result);
  rpc Images(Job) returns (Result);
}

message Job    { bytes json = 1; }  // the job json
message Result { bytes json = 1; }  // the result json
```

Messages carry the same json as HTTP and NATS inside a `bytes` field.
Typed protobuf messages are postponed. Calls are unary and synchronous.

**Auth:** every call needs the metadata `authorization: Bearer
<BEE_TOKEN>`. Without it, the call fails with `UNAUTHENTICATED`.

**Success:** `Result.json` holds the result.

**Failure:** a gRPC status with no `Result`. The status message is
`"<CODE>: <message>"`, and the trailers carry:

- `bee-error-code`: the bee code, e.g. `NO_RENDITION`
- `bee-retryable`: `true` or `false`

Over gRPC, a failed job has no result body: the Go client fills `id`,
`meta` (from the job it sent) and `error`. Partial data, for example audio
`info`, comes only over HTTP and NATS.

| gRPC code | bee codes |
|---|---|
| `INVALID_ARGUMENT` | `INVALID_JOB`, `INVALID_PATH`, `INVALID_DIMENSIONS`, `INVALID_QUALITY`, `UNSUPPORTED_CODEC`, `UNSUPPORTED_FORMAT`, `INVALID_AUDIO`, `INVALID_VIDEO`, `INVALID_IMAGE`, `INVALID_SOUND`, `INVALID_WATERMARK`, `SOURCE_TOO_LARGE`, `DIMENSION_LIMIT_EXCEEDED`, `PIXEL_LIMIT_EXCEEDED`, `DURATION_LIMIT_EXCEEDED`, `NO_RENDITION`, `OUTPUT_TOO_LARGE` |
| `FAILED_PRECONDITION` | `SOURCE_URL_EXPIRED` |
| `DEADLINE_EXCEEDED` | `DOWNLOAD_TIMEOUT`, `STORAGE_TIMEOUT` |
| `UNAVAILABLE` | `SOURCE_UNAVAILABLE`, `STORAGE_UNAVAILABLE`, `INTERNAL_TEMPORARY_ERROR` |
| `INTERNAL` | `ENCODE_FAILED`, `VERIFY_FAILED`, any other |
| `UNAUTHENTICATED` | missing or wrong token |

The server has no reflection, so pass the proto file. `bytes` is base64 in
grpcurl's json:

```sh
grpcurl -plaintext -proto pkg/proto/bee.proto \
  -H "authorization: Bearer $BEE_TOKEN" \
  -d "{\"json\": \"$(base64 < job.json | tr -d '\n')\"}" \
  bee:9090 bee.v1.Bee/Images
```

## NATS JetStream

NATS works as **send and forget**: you publish a job and go on; bee does
it and publishes the result; a worker of yours reads the results. The NATS
server must have JetStream on (`nats-server -js`).

### Settings

```sh
BEE_NATS_URL=nats://nats:4222   # the nats server; empty: NATS is off
BEE_NATS_TOKEN=                 # auth token of the nats server; empty: none
BEE_NATS_JOBS=bee.jobs          # subject of every job
BEE_NATS_DONE=bee.done          # subject of every result
BEE_NATS_ACK_WAIT=1m            # a job with no sign of life this long goes to another bee
BEE_NATS_MAX_DELIVER=5          # deliveries of one job; the error of the last one is final
BEE_NATS_RETRY_DELAY=10s        # delay before the second delivery, doubled after each
BEE_NATS_RETRY_MAX_DELAY=5m     # longest delay between deliveries
BEE_NATS_STORAGE=file           # file or memory, for both streams
BEE_NATS_REPLICAS=1             # stream replicas, 1..5
BEE_NATS_MAX_AGE=0s             # a job nobody took is deleted after this; 0s: never
BEE_NATS_RESULT_TTL=24h         # how long results are kept
```

`BEE_TOKEN` is not used on NATS.

### Two subjects

bee uses only two subjects. The kind and the id of a job travel inside the
message, not in the subject.

```
bee.jobs    you publish every job here       (BEE_NATS_JOBS)
bee.done    bee publishes every result here  (BEE_NATS_DONE)
```

A subject is made of tokens of `A-Z`, `a-z`, `0-9`, `_` and `-` joined by
dots, with no wildcards (`*`, `>`). The two subjects must not be equal, and
one must not lie inside the other (`media` and `media.done` is not
allowed). bee checks this at start.

### Derived names

bee creates (or updates) two streams and one consumer at start. Their names
come from the subjects: dots become `_`, letters become upper case.

```
BEE_JOBS    stream of the jobs (from bee.jobs): a work queue, a job is deleted once done
BEE_DONE    stream of the results (from bee.done): every result kept BEE_NATS_RESULT_TTL
BEE_JOBS    durable consumer of bee on the stream BEE_JOBS, shared by every bee
```

With `BEE_NATS_JOBS=bee.hetzner.jobs` the job stream and the consumer are
`BEE_HETZNER_JOBS`.

### The message: one envelope both ways

Every message is a json **envelope**. A job:

```json
{
  "kind": "images",
  "job": {
    "id": 42,
    "meta": {"user": 7},
    "input": "https://example.com/photo.jpg",
    "tasks": [
      {"id": "small", "output": "photos/1/small.webp",
       "profile": {"format": "webp", "quality": 80, "max_width": 320}}
    ]
  }
}
```

- `kind`: `audio`, `stream`, `video` or `images`.
- `job`: the job json of that kind, exactly as over HTTP
  ([audio](jobs-audio.md), [video](jobs-video.md), [images](jobs-images.md)).

The result of that job, on the done subject:

```json
{
  "kind": "images",
  "id": 42,
  "result": {
    "id": 42,
    "meta": {"user": 7},
    "tasks": [
      {"id": "small", "output": "photos/1/small.webp", "format": "webp",
       "width": 320, "height": 240, "size": 198, "quality": 80,
       "sha256": "fcd4954b...", "elapsed": 159}
    ],
    "elapsed": 399,
    "sha256": "6ce75867...",
    "source_size": 5599,
    "source_format": "jpg",
    "source_width": 640,
    "source_height": 480
  }
}
```

- `kind`: the kind of the job.
- `id`: the id of the job, exactly as sent.
- `result`: the result json of that kind, exactly as over HTTP.

A failed job has `error` in its result:

```json
{
  "kind": "images",
  "id": 43,
  "result": {
    "id": 43,
    "meta": {"user": 7},
    "elapsed": 0,
    "error": {"code": "INVALID_QUALITY", "message": "task small: quality is required for a lossy webp", "retryable": false}
  }
}
```

### Without the Go client: the nats CLI

The Go client is in [clients.md](clients.md#nats). Any other language does
the same with a NATS library; here it is with the `nats` CLI and the default
subjects.

**Watch results** as they come (a plain subscription: it sees only the
results published while it runs):

```sh
nats sub bee.done
```

**Send a job.** `nats pub` publishes it; `nats req` does the same and
prints the JetStream answer, so you know the job is stored:

```sh
nats req bee.jobs '{"kind":"images","job":{"id":"photo-1","input":"https://example.com/photo.jpg","tasks":[{"id":"small","output":"photos/1/small.webp","profile":{"format":"webp","quality":80,"max_width":320}}]}}'
# {"stream":"BEE_JOBS","seq":1}
```

**Read results reliably** with a durable consumer of your own. NATS
remembers which results it gave to it, so nothing is missed while your
service is down:

```sh
# once: a consumer "my-service" on the results stream
nats consumer add BEE_DONE my-service --pull --deliver all --ack explicit --filter bee.done --defaults

# take the next result (and ack it)
nats consumer next BEE_DONE my-service

# up to 10 results, only the json
nats consumer next BEE_DONE my-service --count 10 --raw
```

In a program: pull from the consumer, handle the result, then ack it. A
result you do not ack comes again. Several instances with the same consumer
name share the results; another consumer name gets its own copy of every
result. `--deliver all` also gives the results that are already in the
stream (kept `BEE_NATS_RESULT_TTL`).

The Go client does the same for you: `SubscribeAudio`, `SubscribeStream`,
`SubscribeVideo` and `SubscribeImages` create durable consumers named
`<done stream>_<GROUP>_<KIND>`, for example `BEE_DONE_BILLING_AUDIO`
([clients.md](clients.md#read-the-results)). Pick other consumer names for
your own consumers.

### Delivery semantics

The job stream is a work queue, and every bee with the same subjects pulls
from one shared durable consumer. Each job goes to one bee at a time. A
bee pulls a job only when it has a free slot (`BEE_CONCURRENCY`). The
slots are shared with HTTP and gRPC: a NATS job that finds them all taken
fails with `INTERNAL_TEMPORARY_ERROR` and is retried as below.

While a job runs, bee marks its message *in progress* every
`BEE_NATS_ACK_WAIT / 3`, so long jobs keep their delivery. If a bee dies,
the message goes to another bee after `BEE_NATS_ACK_WAIT`.

A delivery ends in one of three ways:

- **Done, or failed with a final code:** bee publishes the result on the
  done subject, waits for JetStream to confirm it, then acks the job.
- **Failed with a temporary code, and this is not the last delivery:** bee
  publishes nothing and gives the job back with a delay of
  `BEE_NATS_RETRY_DELAY × 2^(n−1)`, at most `BEE_NATS_RETRY_MAX_DELAY`. With
  the defaults: 10s, 20s, 40s, 80s. Delivery number `BEE_NATS_MAX_DELIVER`
  publishes its failure.
- **Still running at shutdown:** running jobs get `BEE_SHUTDOWN_TIMEOUT`,
  then are cancelled and given back at once without a result, so another
  bee takes them.

More:

- **Temporary codes** (delivered again): `DOWNLOAD_TIMEOUT`,
  `SOURCE_UNAVAILABLE`, `STORAGE_TIMEOUT`, `STORAGE_UNAVAILABLE` and
  `INTERNAL_TEMPORARY_ERROR`.
- **`SOURCE_URL_EXPIRED`** is `retryable` but not delivered again: its
  failure is published at once. Send a new job with a fresh url.
- **Delivery count:** every delivery counts, including those after an ack
  wait or a shutdown.
- **Unconfirmed result:** if JetStream does not confirm the result
  publish, the job is given back and runs again. A result is never lost,
  but it can be published twice.

### Redelivery

A job delivered again (lost ack, crashed bee) runs again, writes over its
keys and publishes its result again on the done subject. Your result
handler may see the same job id twice; make it safe for that.

### Malformed messages

- **Not a json envelope, or an unknown `kind`:** the message is dropped
  (terminated) and logged. Nothing is published.
- **A job without an id, or with an id that breaks the [rule](#transports-the-wire-protocol)**
  (a string of 1..128 printable characters or a whole number): dropped and logged. Nothing is published: there
  is no id to report to.
- **A job with a valid id that is otherwise not valid** (bad fields, bad
  urls, bad keys, `meta` over 16 KB): the failure is published on the done
  subject, usually `INVALID_JOB`.

bee logs only the size of a dropped message, never its body (it may hold a
presigned url). Nobody hears about a dropped message, so check the job
before you publish; the Go client does this for you.

### Several groups of bees on one NATS

Bees with the same subjects share one queue and must share one storage:
any of them may take any job. To run groups with different buckets on the
same NATS server, give each group its own two subjects.

Group "eu":

```sh
BEE_NATS_JOBS=eu.jobs    # stream and consumer EU_JOBS
BEE_NATS_DONE=eu.done    # stream EU_DONE
```

Group "us":

```sh
BEE_NATS_JOBS=us.jobs    # stream and consumer US_JOBS
BEE_NATS_DONE=us.done    # stream US_DONE
```

Clients send to a group with the same subjects: `proto.Names{Jobs:
"eu.jobs", Done: "eu.done"}` in Go, or the subjects directly.

Avoiding name conflicts is your job. If a subject is already held by
another stream, NATS refuses the stream. bee then does not start, and its
error names the stream and the subject, e.g.:

```
nats: stream EU_JOBS of subjects eu.jobs (another stream may hold them): …
```

## Health server

Always on at `BEE_HEALTH_ADDR` (default `:8081`), whatever transports are
on. It has no token, so keep it on an internal network.

| request | answer |
|---|---|
| `GET /health` | Liveness: `200 {"ok":true}` while the process answers. |
| `GET /ready` | Readiness: ffmpeg and vips answer, the storage pings, and NATS (when on) is connected. Answers `200 {"ok":true}`, or `503 {"ok":false,"error":"…"}` with the first error. |
| `GET /metrics` | Prometheus text format. |

### Metrics

`type` is `audio`, `stream`, `video` or `images`.

| metric | kind | meaning |
|---|---|---|
| `bee_jobs_total{type}` | counter | Jobs accepted. |
| `bee_jobs_completed_total{type}` | counter | Jobs completed. |
| `bee_jobs_failed_total{type,code}` | counter | Jobs failed, by error code. |
| `bee_jobs_retried_total` | counter | NATS redeliveries received. |
| `bee_jobs_active` | gauge | Jobs running now. |
| `bee_job_duration_seconds{type}` | histogram | Whole jobs. |
| `bee_processing_seconds{type}` | histogram | ffmpeg / vips time. |
| `bee_download_bytes_total` | counter | Bytes downloaded. |
| `bee_upload_bytes_total` | counter | Bytes uploaded. |

Histogram buckets (seconds): 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300
and 600.

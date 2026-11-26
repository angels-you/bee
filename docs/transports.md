# Transports: the wire protocol

Bee accepts the same jobs over HTTP, gRPC and NATS JetStream. Any
combination can be on at once, and all of them share one engine, one set of
job slots (`BEE_CONCURRENCY`) and one storage. This page describes the
protocol for clients that do not use the Go packages in `pkg/clients` (see
[clients.md](clients.md)).

On every transport, a job is the json of its type, and so is the answer:

| kind | job json | result json | page |
|---|---|---|---|
| `audio` | `audio.Job` | `audio.Result` | [jobs-audio.md](jobs-audio.md) |
| `stream` | `video.Stream` | `video.Result` | [jobs-video.md](jobs-video.md) |
| `video` | `video.Video` | `video.Result` | [jobs-video.md](jobs-video.md) |
| `images` | `images.Job` | `images.Result` | [jobs-images.md](jobs-images.md) |

**Job id rule.** On every transport, a job `id` is 1..128 characters of
`A-Z a-z 0-9 _ -` (uuid, ulid and numbers fit). Bee holds every transport
to this rule because NATS puts the id in a subject.

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

Over gRPC, a failed job's partial result (for example audio `meta`) is not
returned. Use HTTP or NATS if you need it.

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

Enabled by `BEE_NATS_URL` (e.g. `nats://nats:4222`). The NATS server must
have JetStream on.

| env | default | meaning |
|---|---|---|
| `BEE_NATS_URL` | — | Server url. Empty turns NATS off. |
| `BEE_NATS_TOKEN` | — | Auth token. Empty means none. |
| `BEE_NATS_JOBS` | `bee.jobs.new` | Subject base of jobs. |
| `BEE_NATS_DONE` | `bee.jobs.done` | Subject base of results. |
| `BEE_NATS_ACK_WAIT` | `1m` | A delivery with no sign of life for this long goes to another bee. |
| `BEE_NATS_MAX_DELIVER` | `5` | Deliveries of one job. The error of the last one is final. |
| `BEE_NATS_RETRY_DELAY` | `10s` | Nak delay after the first delivery. Doubles after each. |
| `BEE_NATS_RETRY_MAX_DELAY` | `5m` | Longest nak delay. |
| `BEE_NATS_STORAGE` | `file` | `file` or `memory`, for the streams. |
| `BEE_NATS_REPLICAS` | `1` | 1..5. |
| `BEE_NATS_MAX_AGE` | `0s` | An unprocessed job is deleted after this long. 0 means never. |
| `BEE_NATS_RESULT_TTL` | `24h` | How long results are kept. |

### Subjects

Each base is tokens of `A-Z a-z 0-9 _ -` joined by dots, with no
wildcards. The two bases must differ, and neither may lie inside the other.

| what | subject | default example |
|---|---|---|
| a job of a kind (publish here) | `<jobs>.<kind>` | `bee.jobs.new.audio` |
| the result of one job | `<done>.<kind>.<id>` | `bee.jobs.done.audio.track-1001` |
| results of one kind | `<done>.<kind>.*` | `bee.jobs.done.audio.*` |
| all results | `<done>.>` | `bee.jobs.done.>` |

`<kind>` is `audio`, `stream`, `video` or `images`.

### Derived names

Bee creates or updates these at start. JetStream names are the base with
dots changed to `_`, in upper case:

| object | name | default | config |
|---|---|---|---|
| job stream | `<jobs>` as a name | `BEE_JOBS_NEW` | subjects `<jobs>.*`, work-queue retention, `BEE_NATS_MAX_AGE` |
| result stream | `<done>` as a name | `BEE_JOBS_DONE` | subjects `<done>.>`, limits retention, kept `BEE_NATS_RESULT_TTL` |
| consumer | the job stream name | `BEE_JOBS_NEW` | durable pull, explicit ack, `BEE_NATS_ACK_WAIT`; bee counts deliveries itself |

### Delivery semantics

The job stream is a work queue, and every bee with the same bases pulls
from one shared durable consumer. Each job goes to one bee at a time. A
bee pulls a job only when it has a free slot (`BEE_CONCURRENCY`). The
slots are shared with HTTP and gRPC: a NATS job that finds them all taken
fails with `INTERNAL_TEMPORARY_ERROR` and is retried as below.

While a job runs, bee marks its message *in progress* every
`BEE_NATS_ACK_WAIT / 3`, so long jobs keep their delivery. If a bee dies,
the message goes to another bee after `BEE_NATS_ACK_WAIT`.

A delivery ends in one of three ways:

| case | what bee does |
|---|---|
| Done, or failed with a final code | Publishes the result on `<done>.<kind>.<id>`, waits for JetStream to confirm it, then acks the job. |
| Failed with a redeliverable code, and the delivery number is below `BEE_NATS_MAX_DELIVER` | Publishes no result and naks with a delay of `RETRY_DELAY × 2^(n−1)`, capped at `RETRY_MAX_DELAY`. With the defaults: 10s, 20s, 40s, 80s. Delivery number `MAX_DELIVER` publishes its failure. |
| Still running at shutdown | Running jobs get `BEE_SHUTDOWN_TIMEOUT`, then are cancelled and nak'ed at once without a result, so another bee takes them. |

- **Redeliverable codes:** `DOWNLOAD_TIMEOUT`, `SOURCE_UNAVAILABLE`,
  `STORAGE_TIMEOUT`, `STORAGE_UNAVAILABLE` and `INTERNAL_TEMPORARY_ERROR`.
- **`SOURCE_URL_EXPIRED`** is `retryable` but not redelivered: its failure
  is published at once. Send a new job with a fresh url.
- **Delivery count:** every delivery counts, including those after an ack
  wait or a shutdown.
- **Unconfirmed result:** if JetStream does not confirm the result
  publish, the job is nak'ed and runs again. A result is never lost, but
  it can be published twice.

### Redelivery

A redelivered job (lost ack, crashed bee) runs again, writes over its keys
and publishes its result again on `<done>.<kind>.<id>`.

### Malformed messages

| message | outcome |
|---|---|
| Subject `<jobs>.<x>` where `x` is not a known kind | Terminated, nothing published. |
| Body with no readable id, or an id that breaks the rule | Terminated, nothing published. Bee logs only the byte count, never the body. |
| Any other job that is not valid (bad json, bad fields) | Failure published on `<done>.<kind>.<id>`, usually `INVALID_JOB`. |

A waiting client sees nothing for a terminated job, so validate the id
before you publish.

### Sending a job without the Go client

1. Pick a unique job id. Reuse an id only to retry the same job.
2. Start listening for the result **before** you publish, so you cannot
   miss it. Either:
   - subscribe to `<done>.<kind>.<id>` (a plain NATS subscription also
     receives messages stored by the stream), or
   - create an ordered JetStream consumer on stream `<DONE>` with filter
     subject `<done>.<kind>.<id>` and deliver policy *new*.
3. Publish the job json to `<jobs>.<kind>` as a JetStream publish, and
   check the publish ack (`{"stream":"BEE_JOBS_NEW","seq":…}`).
4. Wait for one message. It is the result json; check its `error`.

If you stop waiting, the job still runs. Its result stays in the result
stream for `BEE_NATS_RESULT_TTL`. Read it later with a consumer filtered to
`<done>.<kind>.<id>` and deliver policy *last*, or a direct get of the last
message for that subject.

With the `nats` CLI and default bases:

```sh
# 1. listen first
nats sub 'bee.jobs.done.images.photo-77' --count 1 &

# 2. publish; a request to a stream subject returns the JetStream ack
nats req bee.jobs.new.images "$(cat job.json)"

# later: the last result of the job
nats stream get BEE_JOBS_DONE --last-for 'bee.jobs.done.images.photo-77'
```

### Several bee groups on one NATS

Bees with the same bases share one queue and must share one storage: any
of them may take any job. To run groups with different buckets on the same
NATS server, give each group its own bases:

| group | `BEE_NATS_JOBS` | `BEE_NATS_DONE` | job stream / consumer | result stream |
|---|---|---|---|---|
| default | `bee.jobs.new` | `bee.jobs.done` | `BEE_JOBS_NEW` | `BEE_JOBS_DONE` |
| eu | `eu.jobs.new` | `eu.jobs.done` | `EU_JOBS_NEW` | `EU_JOBS_DONE` |
| us | `us.jobs.new` | `us.jobs.done` | `US_JOBS_NEW` | `US_JOBS_DONE` |

Clients send to a group with the same bases: `proto.Names{Jobs, Done}` in
Go, or the subjects above directly.

Avoiding name conflicts is your job. If a group's subjects overlap another
stream's subjects, or its names collide with existing streams, NATS refuses
the stream. Bee then does not start, and its error names the stream and
subjects, e.g.:

```
nats: stream EU_JOBS_NEW of subjects eu.jobs.new.* (another stream may hold them): …
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

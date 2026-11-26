# Errors

Every failed job has exactly one error code. The codes are the same for
audio, video and images and for every transport. They live in
[`pkg/types/code`](../pkg/types/code): the list `code` and the generated
`code.go`.

## The Error

```go
// pkg/types/code
type Error struct {
	Code      Code   `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Err       error  `json:"-"`
}
```

In json:

```json
{"code": "SOURCE_URL_EXPIRED", "message": "input: source url expired", "retryable": true}
```

- `code` is one of the codes below.
- `message` is a short human text for logs. Do not parse it.
- `retryable` is a copy of `Code.Retryable()`, so a client in another
  language needs no code table.
- `Err` (the cause: ffmpeg output, a storage error) stays inside bee and is
  only logged. It is never sent to clients.

A failed job answers the **result** of its type (`audio.Result`,
`video.Result`, `images.Result`) with `id`, `user_meta` and `error` set and no outputs:

```json
{"id": "track-1", "user_meta": {"album": 7}, "error": {"code": "INVALID_AUDIO", "message": "the audio has no audio stream", "retryable": false}}
```

None of the outputs of a failed job count, even if some objects were
written before the failure.

## Retryable and redeliver

Two properties of every code:

- **retryable**: trying again may succeed. For a client: retry later
  (with backoff) or tell the user. Not retryable: the same job will fail
  again; fix the job or the source.
- **redeliver** (NATS only): bee itself gives the **same** message to a bee
  again. Only codes where the same job can succeed later. See
  [NATS](#nats).

`SOURCE_URL_EXPIRED` is the one code that is retryable but not
redelivered: the same job carries the same expired url, so retry with a
**new job** that has a fresh presigned url.

## Codes

| Code | Meaning | Retryable | Redeliver | HTTP | gRPC |
|---|---|---|---|---|---|
| `DOWNLOAD_TIMEOUT` | A download by a url of the job (source, sound, watermark) did not finish within `BEE_DOWNLOAD_TIMEOUT` | yes | yes | 504 | `DeadlineExceeded` |
| `SOURCE_URL_EXPIRED` | A presigned url expired (`403`/`400` that mentions "expire"): the same job fails again, send a new job with a fresh url | yes | no | 410 | `FailedPrecondition` |
| `SOURCE_UNAVAILABLE` | A download failed: network error, access denied, an unexpected http status or an empty body | yes | yes | 424 | `Unavailable` |
| `STORAGE_TIMEOUT` | A write to the storage or the check of an output key timed out | yes | yes | 504 | `DeadlineExceeded` |
| `STORAGE_UNAVAILABLE` | A write to the storage or the check of an output key failed | yes | yes | 502 | `Unavailable` |
| `INTERNAL_TEMPORARY_ERROR` | Busy (all slots taken within `BEE_QUEUE_WAIT`), cancelled, timed out (the type timeout) or an unexpected failure of bee | yes | yes | 503 | `Unavailable` |
| `INVALID_JOB` | The job is malformed: id, urls, options, profiles, tasks; a source url that is not allowed (scheme, private address, host not in `BEE_SOURCE_HOSTS`) | no | no | 400 | `InvalidArgument` |
| `INVALID_PATH` | An output key is empty, not a clean relative key, has the wrong extension or is used twice | no | no | 400 | `InvalidArgument` |
| `INVALID_DIMENSIONS` | A size of the job is odd, negative, missing or above the limit of bee | no | no | 400 | `InvalidArgument` |
| `INVALID_QUALITY` | A quality is outside 0..100 or missing where it is required | no | no | 400 | `InvalidArgument` |
| `UNSUPPORTED_CODEC` | The job asks for a codec, pixel format, preset or image format bee does not encode | no | no | 422 | `InvalidArgument` |
| `UNSUPPORTED_FORMAT` | The container or codec of the source is one bee does not accept | no | no | 422 | `InvalidArgument` |
| `INVALID_AUDIO` | The audio is empty, corrupted, silent or has no audio stream | no | no | 422 | `InvalidArgument` |
| `INVALID_VIDEO` | The video is empty, corrupted, has no video stream or cannot be decoded | no | no | 422 | `InvalidArgument` |
| `INVALID_IMAGE` | The image is empty, corrupted or cannot be decoded | no | no | 422 | `InvalidArgument` |
| `INVALID_SOUND` | The external sound of a video job is empty, corrupted or has no audio stream | no | no | 422 | `InvalidArgument` |
| `INVALID_WATERMARK` | The watermark is empty, corrupted or not a still image | no | no | 422 | `InvalidArgument` |
| `SOURCE_TOO_LARGE` | A downloaded file is bigger than the limit of bee (`BEE_*_MAX_SOURCE_MB`) | no | no | 413 | `InvalidArgument` |
| `DIMENSION_LIMIT_EXCEEDED` | The source frame is larger than the limit of bee on a side | no | no | 422 | `InvalidArgument` |
| `PIXEL_LIMIT_EXCEEDED` | The source image has more pixels than the limit of bee (`BEE_IMAGES_MAX_MEGAPIXELS`) | no | no | 422 | `InvalidArgument` |
| `DURATION_LIMIT_EXCEEDED` | The source or the processed timeline is shorter or longer than the limits of bee | no | no | 422 | `InvalidArgument` |
| `NO_RENDITION` | Without upscale no profile fits the source: every rendition would be larger than the source | no | no | 422 | `InvalidArgument` |
| `OUTPUT_TOO_LARGE` | An output stays above its `max_bytes` even at the lowest quality | no | no | 422 | `InvalidArgument` |
| `ENCODE_FAILED` | ffmpeg or vips failed on an accepted source | no | no | 422 | `Internal` |
| `VERIFY_FAILED` | An output did not pass the check: stream, size, duration or segments | no | no | 422 | `Internal` |

A code that is not in the table (should not happen) is HTTP 500 and gRPC
`Internal`.

## How each transport shows an error

### HTTP

`POST /v1/<kind>` answers the result json with the HTTP status of the
table; `200` when the job is done. The body of a failed job is the result
with `error`.

Errors before a job runs:

| Case | Status | Body |
|---|---|---|
| Bad or missing `Authorization: Bearer` token | 401 | `{"error":{"code":"INVALID_JOB","message":"bad or missing token","retryable":false}}` |
| Body unreadable or above 1 MiB | 400 | `{"error":{"code":"INVALID_JOB",...}}` |
| Body is not json of the job | 400 | `{"id":"<id if readable>","error":{"code":"INVALID_JOB","message":"job is not json: ..."}}` |
| Unknown path or method | 404 / 405 | Go's plain text answer |

### gRPC

A done job returns `Result{json}` with the result. A failed job returns no
result, only an error status:

- the status code from the table;
- the status message `<CODE>: <message>`;
- trailers `bee-error-code: <CODE>` and `bee-retryable: true|false`.

A bad or missing token is `Unauthenticated` without the bee trailers.

### NATS

The result json, with `error` on failure, is published on the done
subject (`BEE_NATS_DONE`, default `bee.done`) inside an envelope
`{"kind":"...","id":<the job id as sent>,"result":{...}}`. What happens to a failed
delivery depends on the code:

| Case | What bee does |
|---|---|
| Done | Publishes the result, then acks. |
| Code with redeliver, and this delivery is below `BEE_NATS_MAX_DELIVER` | Publishes nothing. Naks with a delay: `BEE_NATS_RETRY_DELAY`, doubled each delivery, at most `BEE_NATS_RETRY_MAX_DELAY`. The job runs again, maybe on another bee. |
| Code with redeliver on the last delivery (`BEE_NATS_MAX_DELIVER`) | Publishes the failure, then acks. Final. |
| Code without redeliver | Publishes the failure, then acks. Final. |
| bee is stopping and the job did not finish | Publishes nothing, naks at once: another bee takes it. |
| The job has a valid `id` but is not valid otherwise (also `user_meta` over 16 KB) | Publishes `INVALID_JOB` (or another invalid code), acks. |
| The job has no `id`, or an id that breaks the rule | Terminated and logged; nothing is published (there is no id to report to). |
| The message is not a json envelope, or its `kind` is unknown | Terminated and logged; nothing is published. |

A job id must be a string of 1..128 printable characters (any language,
spaces allowed, no line breaks or control characters) or a whole number,
on every transport. `null`, `true`/`false`, objects, arrays and `""` are
`INVALID_JOB`. A job `user_meta` above 16 KB of json is `INVALID_JOB` too.

bee never checks whether output keys are taken: a job writes over them. A
redelivered job runs again and publishes its result again (see
[transports.md](transports.md#redelivery)).

## Go clients

The HTTP and gRPC Go clients in `pkg/clients` ([clients.md](clients.md))
return the same thing: the result with `Error` set **and** the same
`*code.Error` as the `error`.

```go
res, err := client.GenerateAudio(ctx, job)
if err != nil {
	e := code.As(err)
	switch {
	case e.Code == code.SourceUrlExpired:
		// send a new job with a fresh url
	case e.Retryable:
		// try again later
	default:
		// fix the job or the source; do not retry
	}
}
```

Failures the HTTP and gRPC clients see themselves (bee unreachable, a
broken answer) are `INTERNAL_TEMPORARY_ERROR`, retryable. A job the client
cannot send (not json, an invalid id) and a rejected token are
`INVALID_JOB`. The gRPC client rebuilds the bee code from the trailers.

The NATS client only sends: `GenerateAudio`, `GenerateStream`,
`GenerateVideo` and `GenerateImages` return a `*code.Error` for a job that
is not valid, or a NATS error. The failure of the job comes later to your
handler of `SubscribeAudio` (and the others) with `res.Error` set
([clients.md](clients.md#nats)).

`code.As(err)` turns any error into a `*code.Error`: a typed one as is
(also wrapped), a context deadline or cancel as `INTERNAL_TEMPORARY_ERROR`.

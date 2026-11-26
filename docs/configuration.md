# Configuration

bee is configured only by environment variables with the prefix `BEE_`.
There is no config file inside the image and no flags.

[`cmd/.env`](../cmd/.env) lists every key with its default. A test
(`TestEnvFile` in `internal/config`) checks that the file matches the
defaults in the code, so the file and this page can be trusted as the
reference. Pass it to docker with `--env-file cmd/.env` and change what you
need.

## What is required

Only three things have no default:

1. **The storage**: `BEE_STORAGE` and the keys of the chosen kind
   (`BEE_LOCAL_DIR`, or `BEE_S3_PROVIDER`, `BEE_S3_KEY`, `BEE_S3_SECRET`,
   `BEE_S3_BUCKET`). See [storage.md](storage.md).
2. **At least one transport**: `BEE_HTTP_ADDR`, `BEE_GRPC_ADDR` or
   `BEE_NATS_URL`. See [transports.md](transports.md).
3. **`BEE_TOKEN`** when http or grpc is on.

Everything else has a default.

A minimal local run:

```sh
BEE_STORAGE=local
BEE_LOCAL_DIR=/data/out
BEE_HTTP_ADDR=:8080
BEE_TOKEN=change-me
```

## Syntax

Values are parsed by [envy](https://github.com/monopolly/envy):

| Type | Syntax | Example |
|---|---|---|
| duration | Go duration | `0s`, `90s`, `5m`, `24h` |
| bool | `true` / `false` (also `1`, `0`, `t`, `f`) | `BEE_AUDIO_ANALYSIS=true` |
| number | integer or decimal | `BEE_AUDIO_LUFS=-14` |
| list | comma separated, spaces around items trimmed | `BEE_AUDIO_RENDITIONS=128,64,256` |

An **empty value is the same as an unset key**: the default stays. So you
cannot clear a key that has a non-empty default (for example
`BEE_HEALTH_ADDR=` keeps `:8081`, `BEE_FPCALC=` keeps `fpcalc`). Keys whose
default is empty (`BEE_GRPC_ADDR`, `BEE_NATS_URL`, `BEE_SOURCE_HOSTS`, ...)
are "off" when empty.

The bee binary reads only the real environment. `cmd/.env` is read by docker
(`--env-file`), not by bee.

## Server limits and processing defaults

Keys of each job type fall into two groups (they are also grouped so in
`cmd/.env`):

- **Server limits**: timeouts, sizes, durations, dimensions, threads. A job
  can never change them. A job above a limit fails with a limit code
  (`SOURCE_TOO_LARGE`, `DURATION_LIMIT_EXCEEDED`, ...).
- **Processing defaults**: loudness targets, bitrates, qualities, etc. A job
  field left empty (zero) takes this value; a job that sets the field uses
  its own value (within the validation rules of the job).

`BEE_CONCURRENCY`, `BEE_QUEUE_WAIT`, `BEE_DOWNLOAD_TIMEOUT` and the other
common keys are server limits too.

## How errors are reported

At start bee checks the whole configuration and reports **every** problem
at once, one line per key, on stderr, then exits with code 1:

```
bee: the configuration is not valid:
  config: BEE_STORAGE is required: s3 or local
  config: no transport: set BEE_HTTP_ADDR, BEE_GRPC_ADDR or BEE_NATS_URL
```

A value that cannot be parsed (for example `BEE_CONCURRENCY=many`) is
reported the same way; parse errors are reported before range checks.

After the configuration, bee opens the storage (an S3 bucket is pinged),
connects to NATS (if on) and checks that ffmpeg, ffprobe, vips and
vipsheader work. Any failure there is logged as `bee: start failed` and bee
exits with code 1.

## Keys

### Storage

| Key | Default | Required | Meaning |
|---|---|---|---|
| `BEE_STORAGE` | none | yes | `local` or `s3` |
| `BEE_LOCAL_DIR` | none | with `local` | Root folder of the outputs; created if missing. The image uses `/data/out` (a volume). |
| `BEE_S3_PROVIDER` | none | with `s3` | Provider name from [storage.md](storage.md#providers): `cloudflare`, `hetzner`, `aws`, `minio`, ... |
| `BEE_S3_KEY` | none | with `s3` | Access key |
| `BEE_S3_SECRET` | none | with `s3` | Secret key |
| `BEE_S3_BUCKET` | none | with `s3` | Bucket name |
| `BEE_S3_ENDPOINT` | empty | no | Empty: the endpoint template of the provider |
| `BEE_S3_REGION` | empty | no | Empty: the default region of the provider. Ignored for `cloudflare` (always `auto`). |
| `BEE_S3_ACCOUNT` | empty | no | Account id, when the endpoint template of the provider has `<account>` (cloudflare, upcloud, oracle) |
| `BEE_S3_PREFIX` | empty | no | Folder inside the bucket that holds every key; empty: the bucket root |
| `BEE_S3_SECURE` | `true` | no | https. Counts only for self-hosted providers (`minio`, `ceph`); cloud providers always use https. |

Only the keys of the chosen kind are checked: with `BEE_STORAGE=s3` the
local keys are not required, and the other way round.

### Transports

```sh
BEE_HTTP_ADDR=      # HTTP listen address, e.g. :8080; empty: off. Needs BEE_TOKEN
BEE_GRPC_ADDR=      # gRPC listen address, e.g. :9090; empty: off. Needs BEE_TOKEN
BEE_TOKEN=          # token of HTTP and gRPC: "Authorization: Bearer <token>"; not used by NATS
BEE_HEALTH_ADDR=:8081   # health server, always on, no token: /health, /ready, /metrics
```

At least one transport is required (HTTP, gRPC or NATS). `BEE_TOKEN` is
required when HTTP or gRPC is on. HTTP takes jobs only (`POST /v1/<kind>`).

`cmd/.env` sets `BEE_HTTP_ADDR=:8080` and `BEE_TOKEN=change-me` as an
example; the code defaults for both are empty. Change the token.

### NATS

NATS JetStream is on when `BEE_NATS_URL` is set. The other keys are checked
only then.

```sh
BEE_NATS_URL=                   # e.g. nats://nats:4222; empty: off
BEE_NATS_TOKEN=                 # auth token of the nats server; empty: none
BEE_NATS_JOBS=bee.jobs          # subject of every job
BEE_NATS_DONE=bee.done          # subject of every result
BEE_NATS_ACK_WAIT=1m            # a job with no sign of life this long goes to another bee
BEE_NATS_MAX_DELIVER=5          # deliveries of one job; the failure of the last one is final
BEE_NATS_RETRY_DELAY=10s        # delay before the second delivery, doubled after each
BEE_NATS_RETRY_MAX_DELAY=5m     # longest delay; at least BEE_NATS_RETRY_DELAY
BEE_NATS_STORAGE=file           # where JetStream keeps the streams: file or memory
BEE_NATS_REPLICAS=1             # stream replicas, 1..5, at most the servers in the cluster
BEE_NATS_MAX_AGE=0s             # a job nobody took is deleted after this; 0s: never
BEE_NATS_RESULT_TTL=24h         # how long results are kept
```

A running job tells NATS it is alive every `BEE_NATS_ACK_WAIT / 3`, so a
long job keeps its delivery.

The two subjects:

- are made of tokens of `A-Z`, `a-z`, `0-9`, `_` and `-` joined by dots,
  without wildcards;
- must not be equal, and one must not lie inside the other.

Stream and consumer names come from the subjects (dots to `_`, upper case):
`bee.jobs` gives the job stream and the consumer `BEE_JOBS`, `bee.done`
gives the result stream `BEE_DONE`. Bees with the same subjects share one
queue, so they must write to the same storage; bees with another bucket on
the same NATS need other subjects. See [transports.md](transports.md#nats-jetstream).

The Go client reads results through its own consumers on the result stream,
one per group and kind: `BEE_DONE_BILLING_AUDIO`, `BEE_DONE_BILLING_IMAGES`
and so on ([clients.md](clients.md#read-the-results)). They are not bee
settings.

### Common

| Key | Default | Meaning |
|---|---|---|
| `BEE_WORKER_ID` | `bee-1` | Name of this bee in logs and in the NATS connection |
| `BEE_TEMP_DIR` | `/tmp/bee` | Temporary job folders are created here |
| `BEE_CONCURRENCY` | `2` | Jobs at once, all types together; at least 1 |
| `BEE_QUEUE_WAIT` | `0s` | How long a job waits for a free slot; `0s`: refuse at once with `INTERNAL_TEMPORARY_ERROR` "all job slots are busy" |
| `BEE_DOWNLOAD_TIMEOUT` | `2m` | All downloads of one job together; must be at most the timeout of every type (`BEE_AUDIO_TIMEOUT`, `BEE_VIDEO_TIMEOUT`, `BEE_IMAGES_TIMEOUT`) |
| `BEE_UPLOAD_CONCURRENCY` | `8` | Objects of one job uploaded at once |
| `BEE_SHUTDOWN_TIMEOUT` | `2m` | On SIGTERM/SIGINT, how long running jobs get to finish |
| `BEE_SOURCE_HOSTS` | empty | Allowed source url hosts, comma separated, matched by suffix (`example.com` allows `cdn.example.com`); empty: any public host. See [security.md](security.md). |
| `BEE_ALLOW_INSECURE_SOURCE` | `false` | `true` allows `http://` and private addresses for source urls. Development and tests only. |

### Binaries

A path or a name in `PATH`. The docker image installs all of them.

| Key | Default | Used by |
|---|---|---|
| `BEE_FFMPEG` | `ffmpeg` | audio, video |
| `BEE_FFPROBE` | `ffprobe` | audio, video |
| `BEE_FPCALC` | `fpcalc` | audio fingerprint (chromaprint). Optional: if it is not found, bee logs a warning and fingerprint is off. `BEE_AUDIO_FINGERPRINT=true` without it stops the start. |
| `BEE_VIPS` | `vips` | images |
| `BEE_VIPSHEADER` | `vipsheader` | images |

### Audio

Server limits:

| Key | Default | Meaning |
|---|---|---|
| `BEE_AUDIO_TIMEOUT` | `5m` | One job, from download to upload |
| `BEE_AUDIO_MAX_SOURCE_MB` | `500` | Largest original, MiB |
| `BEE_AUDIO_MIN_DURATION` | `1s` | Shortest original |
| `BEE_AUDIO_MAX_DURATION` | `30m` | Longest original; above `BEE_AUDIO_MIN_DURATION` |

Defaults of empty job fields (see [jobs-audio.md](jobs-audio.md)):

| Key | Default | Meaning |
|---|---|---|
| `BEE_AUDIO_LUFS` | `-14` | Loudness target, LUFS; below 0 and above -70 |
| `BEE_AUDIO_TRUE_PEAK` | `-1.5` | True peak ceiling, dBTP; at most 0 |
| `BEE_AUDIO_RENDITIONS` | `128,64,256` | AAC bitrates of the HLS renditions, kbit/s, in master playlist order; positive and distinct |
| `BEE_AUDIO_SAMPLE_RATE` | `44100` | Hz of each rendition |
| `BEE_AUDIO_CHANNELS` | `2` | Channels of each rendition |
| `BEE_AUDIO_SEGMENT` | `6` | HLS segment length, seconds |
| `BEE_AUDIO_WAVEFORM_PPS` | `100` | Points per second in the waveform json |
| `BEE_AUDIO_WAVEFORM_POINTS` | `512` | Points in the waveform of the result |
| `BEE_AUDIO_ANALYSIS` | `false` | Quality diagnostics (astats, silencedetect) |
| `BEE_AUDIO_FINGERPRINT` | `false` | Chromaprint via fpcalc, in the result only; needs `BEE_FPCALC` |
| `BEE_AUDIO_SILENCE_DB` | `-60` | Silence threshold, dB; below 0 |
| `BEE_AUDIO_SILENCE_SEC` | `2` | Shortest silence in the report, seconds; positive |

### Video

Server limits:

| Key | Default | Meaning |
|---|---|---|
| `BEE_VIDEO_TIMEOUT` | `10m` | One job, from download to upload |
| `BEE_VIDEO_MAX_SOURCE_MB` | `500` | Each downloaded file (video, sound, watermark), MiB |
| `BEE_VIDEO_MIN_DURATION` | `1s` | Shortest original and result |
| `BEE_VIDEO_MAX_DURATION` | `5m` | Longest original and result; above `BEE_VIDEO_MIN_DURATION` |
| `BEE_VIDEO_MAX_DIMENSION` | `3840` | Longest frame side of the original and the result, px; at least 2 |
| `BEE_VIDEO_THREADS` | `4` | Threads of one ffmpeg run |

Defaults of empty job fields (see [jobs-video.md](jobs-video.md)):

| Key | Default | Meaning |
|---|---|---|
| `BEE_VIDEO_SOUND_BITRATE` | `128` | AAC kbit/s of a profile with `sound_bitrate` 0; `16..512` |
| `BEE_VIDEO_WATERMARK_WIDTH` | `160` | Watermark width when the job gives width 0, px; `1..BEE_VIDEO_MAX_DIMENSION` |
| `BEE_VIDEO_IMAGE_QUALITY` | `80` | Preview image quality when the job gives 0; `1..100` |
| `BEE_VIDEO_LUFS` | `-16` | Loudness target of `options.normalize`; `-70..-5` |
| `BEE_VIDEO_TRUE_PEAK` | `-1.5` | True peak ceiling of `options.normalize`, dBTP; `-9..0` |

### Images

Server limits:

| Key | Default | Meaning |
|---|---|---|
| `BEE_IMAGES_TIMEOUT` | `2m` | One job, from download to upload |
| `BEE_IMAGES_MAX_SOURCE_MB` | `50` | Largest original, MiB |
| `BEE_IMAGES_MAX_MEGAPIXELS` | `100` | Original width x height, millions |
| `BEE_IMAGES_MAX_DIMENSION` | `8000` | Longest side of a result (and of the task box), px |
| `BEE_IMAGES_THREADS` | `4` | Threads of one vips run |

Defaults of empty job fields (see [jobs-images.md](jobs-images.md)):

| Key | Default | Meaning |
|---|---|---|
| `BEE_IMAGES_MIN_QUALITY` | `30` | `max_bytes` never lowers quality below this; `1..100` |
| `BEE_IMAGES_AVIF_EFFORT` | `4` | AVIF encoder effort, `0..9`: higher is slower and smaller |

### Not a setting

`BEE_VERSION` is set by the image at build time and only shown in the start
log.

The job `user_meta` limit (16 KB of json) and the job id rule are fixed: no key
changes them.

# Audio jobs

An audio job takes one track by url and publishes it as HLS: AAC-LC in
fragmented MP4, one rendition per bitrate, under one master playlist. It can
also normalize loudness, encrypt segments (AES-128), and publish a
waveform, metadata and an analysis json.

- Kind: `audio`. HTTP `POST /v1/audio`, gRPC `bee.v1.Bee/Audio`, NATS
  `{"kind":"audio","job":{...}}` on the jobs subject. See [transports.md](transports.md).
- Go types: `github.com/angels-you/bee/pkg/types/audio` (`Job`, `Options`,
  `Result`, `Rendition`, `Warning`, `Metadata`, `Loudness`, `Quality`,
  `Silence`, `Fingerprint`).
- Pipeline: `internal/engine/audio.go`.
- Error codes: [errors.md](errors.md). Env settings: [configuration.md](configuration.md).

## Job

| json | type | required | meaning |
|---|---|---|---|
| `id` | string or number | yes | Job id: a string of 1..128 printable characters (any language, spaces allowed, no line breaks) or a whole number. Returned in the result exactly as sent. |
| `user_meta` | object | no | Your own data. bee never reads it and returns it untouched in the result, on failure too. At most 16 KB of json. |
| `input` | string | yes | Url of the original (presigned GET or any public https). Logged without its query. Scheme and host are checked at download (https only unless `BEE_ALLOW_INSECURE_SOURCE`, host in `BEE_SOURCE_HOSTS` if set). |
| `output` | string | yes | Storage key of the master playlist. Must end in `.m3u8`. |
| `wave` | string | no | Storage key of the waveform json. Empty: no waveform json and no `result.waveform`. |
| `metadata` | string | no | Storage key of the metadata json. Empty: not published. |
| `analysis` | string | no | Storage key of the analysis json. Empty: not published. |
| `key` | bytes (base64 in json) | no | AES-128 key, exactly 16 bytes. Empty: HLS in the clear. Never logged, never stored. |
| `key_uri` | string | with `key` | Key address written into the media playlists, e.g. `skd://1001`. No `"` and no line breaks. |
| `options` | object | no | Processing options, see below. |

Storage keys are relative keys in the bee storage, not urls or bucket
names. One leading `/` is trimmed. The rest must already be clean: no `..`,
no `//`, no spaces, tabs, line breaks, `?`, `#` or `\`. All keys of a job
must be different. `wave`, `metadata` and `analysis` must not lie in a
rendition folder (`<dir of output>/<kbit>/`). A broken key gives
`INVALID_PATH`.

## Options

Each empty field takes the bee default from env. A job cannot change server
limits.

| json | type | default (env) | rule |
|---|---|---|---|
| `normalize` | bool | `false` | Apply a linear gain toward `lufs`, kept under `true_peak`. Without it the gain is 0 dB. |
| `lufs` | number | `BEE_AUDIO_LUFS` (-14) | Loudness target, LUFS. Must be below 0 and above -70. |
| `true_peak` | number | `BEE_AUDIO_TRUE_PEAK` (-1.5) | True peak ceiling, dBTP. Must be below 0. |
| `renditions` | int[] | `BEE_AUDIO_RENDITIONS` (128,64,256) | AAC bitrates, kbit/s. Positive and distinct. The master playlist lists them in this order, and players start with the first. |
| `sample_rate` | int | `BEE_AUDIO_SAMPLE_RATE` (44100) | Hz of each rendition. Must not be negative. |
| `channels` | int | `BEE_AUDIO_CHANNELS` (2) | Channels of each rendition. Must not be negative. |
| `segment` | int | `BEE_AUDIO_SEGMENT` (6) | HLS segment length, seconds. Must not be negative. |
| `waveform_pps` | int | `BEE_AUDIO_WAVEFORM_PPS` (100) | Points per second in the waveform json. Must not be negative. |
| `waveform_points` | int | `BEE_AUDIO_WAVEFORM_POINTS` (512) | Points in `result.waveform`. Must not be negative. |
| `analysis` | bool or null | `BEE_AUDIO_ANALYSIS` (false) | Quality diagnostics (`result.quality`). Absent or `null` uses the default. `false` turns them off. |
| `fingerprint` | bool or null | `BEE_AUDIO_FINGERPRINT` (false) | Chromaprint of the first 120 s (`result.fingerprint`). Absent or `null` uses the default. |

Server-only settings (no job field): `BEE_AUDIO_TIMEOUT` (5m, the whole
job), `BEE_AUDIO_MAX_SOURCE_MB` (500), `BEE_AUDIO_MIN_DURATION` (1s),
`BEE_AUDIO_MAX_DURATION` (30m), `BEE_AUDIO_SILENCE_DB` (-60),
`BEE_AUDIO_SILENCE_SEC` (2), plus the shared `BEE_DOWNLOAD_TIMEOUT`,
`BEE_CONCURRENCY` and `BEE_QUEUE_WAIT`.

Fingerprinting needs `fpcalc`. If a job asks for it and bee has no `fpcalc`,
the job still succeeds and gets a `fingerprint` warning.
`BEE_AUDIO_FINGERPRINT=true` without `fpcalc` stops bee at start.

## Example job

```json
{
  "id": 1001,
  "user_meta": {"album": 7},
  "input": "https://uploads.example.com/raw/1001.wav?X-Amz-Signature=...",
  "output": "tracks/1001/master.m3u8",
  "wave": "tracks/1001/wave.json",
  "metadata": "tracks/1001/meta.json",
  "analysis": "tracks/1001/analysis.json",
  "key": "MDEyMzQ1Njc4OWFiY2RlZg==",
  "key_uri": "skd://1001",
  "options": {
    "normalize": true,
    "lufs": -14,
    "renditions": [128, 64, 256],
    "analysis": true,
    "fingerprint": false
  }
}
```

The only required fields are `id`, `input` and `output`.

## Pipeline

1. Validate the job and fill the defaults. A broken job fails here and
   nothing is downloaded.
2. Take a job slot. If none is free within `BEE_QUEUE_WAIT`, the job fails
   with `INTERNAL_TEMPORARY_ERROR`. Then start the timeout and the temp
   folder.
3. Existing objects at the keys are not checked: the job writes over
   them. A NATS redelivery runs the job again.
4. `download`: the original, with its sha256 and size.
5. `probe`: ffprobe, then the codec and duration limits. Accepted source
   codecs are mp3, flac, pcm (s16/s24/s32/f32 le, s16/s24/f32 be, u8), aac,
   alac, vorbis, opus, wmav2, mp2 and ac3. Any other codec gives
   `UNSUPPORTED_FORMAT`. A duration outside min..max gives
   `DURATION_LIMIT_EXCEEDED`.
6. `analysis`: one decode pass for loudness (EBU R128), quality (if on),
   and PCM for the waveform (if `wave` is set). A track with no sample
   above -144 dB fails with `INVALID_AUDIO` ("track is silent").
7. `fingerprint`: if asked. An error here becomes a warning.
8. `normalize`: if `normalize` is set, compute the gain. It is linear only
   and reduced if the true peak would pass the ceiling.
9. `encode`: one ffmpeg run produces every rendition. Each rendition must
   be cut into exactly the same segments as the first, or the job fails
   with `VERIFY_FAILED`. Output loudness is measured on the first
   rendition.
10. `waveform`: if `wave` is set.
11. `upload`: in waves, with the master playlist last (see below).
12. `verify`: check that the master, each media playlist, `init.mp4`, the
    first and last segment, and each requested json are in storage.

`result.metrics` holds the milliseconds of each stage under these names,
plus `total`.

## Storage layout

For `"output": "tracks/1001/master.m3u8"` with renditions 128, 64 and 256:

```
tracks/1001/master.m3u8          master playlist (output)
tracks/1001/128/index.m3u8       media playlist
tracks/1001/128/init.mp4         fMP4 init, never encrypted
tracks/1001/128/seg_0000.m4s     segments seg_NNNN.m4s
tracks/1001/128/seg_0001.m4s
...
tracks/1001/64/...
tracks/1001/256/...
tracks/1001/wave.json            if wave is set
tracks/1001/meta.json            if metadata is set
tracks/1001/analysis.json        if analysis is set
```

Each rendition folder is `<dir of output>/<kbit>`. The master lists
`<kbit>/index.m3u8` with `BANDWIDTH` (peak), `AVERAGE-BANDWIDTH` (both
measured from the published segments) and `CODECS="mp4a.40.2"`.

Content-Type comes from the extension: `.m3u8`
`application/vnd.apple.mpegurl`, `.mp4` `video/mp4`, `.m4s`
`video/iso.segment`, `.json` `application/json`.

### Json files

- `wave`: `{"version":1,"duration_ms":…,"sample_rate":<points per second>,"channels":1,"min":[…],"max":[…],"rms":[…]}`.
  It is a mono mix, after the normalize gain. `min` and `max` are the peak
  sample values of each bucket in [-1, 1], `rms` is in [0, 1], and all three
  arrays have the same length.
- `metadata`: `{"sha256":"<original>","renditions":[<Rendition>…],"meta":<Metadata>}`
  (inside this file the ffprobe metadata is under `meta`).
- `analysis`: `{"loudness":<Loudness>,"quality":<Quality>,"warnings":[…]}`.
  Only the fields that are set appear: no `quality` without analysis, and no
  `warnings` if there are none.

The fingerprint is never written to storage. It only appears in the
result.

## Encryption

With `key` set, every segment of every rendition is encrypted whole with
AES-128-CBC and PKCS7 padding, using a new random IV per segment, in memory
right before upload. Each media playlist gets this line above each
`#EXTINF`:

```
#EXT-X-KEY:METHOD=AES-128,URI="<key_uri>",IV=0x<32 hex>
```

`init.mp4` stays clear, and the master playlist has no key lines. The key
itself is not written anywhere. Your key server answers `key_uri` with the
16 bytes. `result.encrypted` is `true`.

## Ordering guarantees

Uploads run in waves. Inside a wave, `BEE_UPLOAD_CONCURRENCY` objects
upload at once, and the first error cancels the wave and fails the job.

1. `init.mp4`, segments, and the waveform json.
2. The metadata and analysis json.
3. The media playlists, which need the IVs from wave 1.
4. bee checks that its uploaded files are in storage.
5. The master playlist.

So a master in storage means every file it points to is already there. A
job that fails before step 5 publishes no master. It can leave other
objects behind, and a rerun with the same keys overwrites them.

## Existing keys

bee never checks whether the keys of a job are taken: every job that
arrives runs and writes over whatever lies there, on every transport.
Keeping keys apart is up to you. A NATS redelivery (lost ack, crashed bee)
runs the job again and publishes its result again. See
[transports.md](transports.md#redelivery).

## Result

| json | type | meaning |
|---|---|---|
| `id` | string or number | Job id, exactly as sent. |
| `user_meta` | object | Job `user_meta`, exactly as sent. |
| `elapsed` | int | Milliseconds for the whole job. |
| `sha256` | string | sha256 of the original. |
| `source_size` | int | Bytes of the original. |
| `size` | int | Bytes published in total. |
| `encrypted` | bool | Segments are encrypted with `key`. |
| `renditions` | Rendition[] | In master playlist order. |
| `waveform` | bytes (base64) | `waveform_points` peaks, one byte each (0 is silence, 255 is full scale). Only with `wave`. |
| `metrics` | object | Stage name to milliseconds. |
| `metadata` | Metadata | From ffprobe. |
| `loudness` | Loudness | EBU R128 of the original and of the output. |
| `quality` | Quality | Only with analysis on. |
| `fingerprint` | Fingerprint | Only with fingerprint on and `fpcalc` present. |
| `warnings` | Warning[] | Non-critical notes. |
| `error` | `{code, message, retryable}` | Set only on failure. |

**Rendition:** `folder` (the kbit as text), `bitrate` (AAC target, bit/s),
`average_bandwidth`, `peak_bandwidth` (bit/s, measured over the published
segments), `size` (bytes of the media playlist, init and segments).

**Metadata:** `duration_ms`, `size`, `format` (container, e.g. `mp3`,
`flac`, `wav`, `mov,mp4,m4a,3gp,3g2,mj2`, `ogg`), `codec`, `bitrate`
(bit/s), `sample_rate`, `channels`, `channel_layout`, `bit_depth`, `title`,
`artist`, `album`, `album_artist`, `composer`, `genre`, `date`, `year`,
`track_number`, `disc_number`, `isrc` (upper case, no dashes),
`copyright`, `encoder`, `tags` (raw, lowercase keys, at most 64, values up
to 1024 runes), `sha256`.

**Loudness:** `integrated` (LUFS), `true_peak` (dBTP), `sample_peak` (dBFS),
`lra` (LU), `threshold` (LUFS), `rms` (dBFS), `gain` (dB applied),
`gain_limited`, `output_lufs`, `output_true_peak`, `output_lra`. Every value
is finite. Silence reports -144 dB for levels and -70 LUFS for loudness.

**Quality:** `silences` (`[{start, end, duration}]`, seconds),
`leading_silence`, `trailing_silence`, `clipping`, `flat_factor`,
`channel_rms` (dBFS per channel), `channel_imbalance` (dB),
`dc_offset`, `dynamic_range` (sample peak minus rms, dB).

**Fingerprint:** `algorithm` (`chromaprint`), `version`, `fingerprint`,
`duration` (seconds analyzed), `max_seconds`.

On failure, the result keeps what bee already knew: `sha256`,
`source_size`, `user_meta`, `metadata`, `loudness`, `quality`, `warnings` and `metrics`.
`renditions`, `waveform` and `size` are cleared, and nothing in the result
counts as published. Over gRPC, a failure carries only `id`, `user_meta` and `error` (see
[transports.md](transports.md#grpc)).

### Example result

`waveform` is shortened here. The real value holds `waveform_points` bytes.

```json
{
  "id": 1001,
  "user_meta": {"album": 7},
  "elapsed": 8423,
  "sha256": "9f2c…e1",
  "source_size": 41254012,
  "size": 12873311,
  "encrypted": true,
  "renditions": [
    {"folder": "128", "bitrate": 128000, "average_bandwidth": 131204, "peak_bandwidth": 134880, "size": 3871022},
    {"folder": "64", "bitrate": 64000, "average_bandwidth": 66110, "peak_bandwidth": 68320, "size": 1951733},
    {"folder": "256", "bitrate": 256000, "average_bandwidth": 259870, "peak_bandwidth": 266512, "size": 7050556}
  ],
  "waveform": "AAMKERkhKjM8RU5XYGlye4SNlp+osbrDzNXe5/D5",
  "metrics": {"download": 912, "probe": 61, "analysis": 1840, "fingerprint": 0, "normalize": 0, "encode": 4012, "waveform": 95, "upload": 1388, "verify": 104, "total": 8423},
  "metadata": {
    "duration_ms": 233040, "size": 41254012, "format": "wav", "codec": "pcm_s16le",
    "bitrate": 1411200, "sample_rate": 44100, "channels": 2, "channel_layout": "stereo",
    "bit_depth": 16, "title": "Song", "artist": "Artist", "sha256": "9f2c…e1"
  },
  "loudness": {
    "integrated": -9.8, "true_peak": 0.4, "sample_peak": -0.1, "lra": 6.2, "threshold": -20.1, "rms": -12.3,
    "gain": -4.2, "gain_limited": false, "output_lufs": -14.0, "output_true_peak": -3.6, "output_lra": 6.1
  },
  "quality": {
    "leading_silence": 0.2, "trailing_silence": 1.4, "clipping": false, "flat_factor": 0,
    "channel_rms": [-12.1, -12.6], "channel_imbalance": 0.5, "dc_offset": 0.0001, "dynamic_range": 12.2
  },
  "warnings": [{"code": "clipping", "message": "digital clipping: flat factor 3.1 dB at full scale"}]
}
```

## Warnings

A warning never fails a job. Codes:

| code | when |
|---|---|
| `low_bitrate` | Lossy source (mp3, aac, vorbis, opus) below 128 kbit/s. |
| `low_sample_rate` | Source below 44100 Hz. |
| `silent` | Integrated loudness below -60 LUFS. |
| `too_quiet` | Integrated loudness below -30 LUFS. |
| `clipping` | A full scale peak with a flat factor above 0 dB (analysis on). |
| `leading_silence` | 5 s or more of silence at the start (analysis on). |
| `trailing_silence` | 5 s or more of silence at the end (analysis on). |
| `long_silence` | A silence of 10 s or more inside the track. Reported once (analysis on). |
| `channel_imbalance` | Channels differ by 6 dB or more (analysis on). |
| `dc_offset` | Absolute DC offset of 0.05 or more (analysis on). |
| `fingerprint` | The fingerprint failed, or there is no `fpcalc`. |
| `gain_limited` | The true peak ceiling reduced the normalize gain. |
| `output_measure` | Output loudness could not be measured. |
| `true_peak` | With `normalize`: output true peak more than 0.5 dB above `true_peak`. |

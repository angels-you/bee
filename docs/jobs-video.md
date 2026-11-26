# Video jobs

There are two video job types, and both use the same options and pipeline:

| kind | Go type | output | HTTP | gRPC | NATS envelope |
|---|---|---|---|---|---|
| `stream` | `video.StreamJob` | HLS VOD: one or more renditions | `POST /v1/stream` | `bee.v1.Bee/Stream` | `{"kind":"stream","job":{...}}` |
| `video` | `video.VideoJob` | one progressive MP4 | `POST /v1/video` | `bee.v1.Bee/Video` | `{"kind":"video","job":{...}}` |

Either job can also make still thumbnails and a short MP4 preview. Both
answer `video.Result`.

- Go types: `github.com/angels-you/bee/pkg/types/video` (`job.go`,
  `presets.go`).
- Validation and defaults: `internal/engine/videocheck.go`. Pipeline:
  `internal/engine/video.go`.
- Error codes: [errors.md](errors.md). Env settings: [configuration.md](configuration.md).
  Wire protocol: [transports.md](transports.md).

Units: bitrates in decimal kbit/s, time in seconds, sizes in pixels, file
sizes in bytes.

## Stream (HLS)

| json | type | required | meaning |
|---|---|---|---|
| `id` | string or number | yes | A string of 1..128 printable characters (any language, spaces allowed, no line breaks) or a whole number. Returned in the result exactly as sent. |
| `meta` | object | no | Your own data. bee never reads it and returns it untouched in the result, on failure too. At most 16 KB of json. |
| `input` | string | yes | Url of the source video. https only (http only with `BEE_ALLOW_INSECURE_SOURCE`). The host must be in `BEE_SOURCE_HOSTS` when that is set. |
| `playlist_path` | string | yes | Key of the entry playlist, ending in `.m3u8`. |
| `options` | Options | no | Applies to every rendition. |
| `profiles` | Profile[] | yes, at least 1 | Renditions. Names are unique, `fps` is required, and every profile must have the aspect ratio of the first (within 1 %). |
| `hls` | HLS | no | Packaging and encryption. |
| `thumbnails` | Thumbnail[] | no | Stills. |
| `preview` | Preview | no | Short MP4. |

## Video (MP4)

| json | type | required | meaning |
|---|---|---|---|
| `id` | string or number | yes | As for Stream. |
| `meta` | object | no | As for Stream. |
| `input` | string | yes | As for Stream. |
| `file_path` | string | yes | Key of the MP4, ending in `.mp4`. |
| `options` | Options | no | |
| `profile` | Profile | yes | The one encoding. `name` is optional and only echoed back. `fps` 0 keeps the source rate. |
| `thumbnails` | Thumbnail[] | no | |
| `preview` | Preview | no | |

An MP4 is never encrypted.

## Options

Processing order: autorotate → trim → rotate/mirror → resize/crop →
visual filters → speed → audio mix/normalize/fades → encode.

| json | type | default | rule |
|---|---|---|---|
| `vertical` | bool | false | Every profile must be 9:16. A side may be off by the rounding to an even pixel. |
| `resize` | string | `contain`, or `cover` when `vertical` | `cover` crops, `contain` pads, `stretch` distorts. |
| `gravity` | string | `center` | `center`, `top`, `bottom`, `left`, `right`, `top-left`, `top-right`, `bottom-left` or `bottom-right`. |
| `upscale` | bool | false | Allow outputs larger than the source. |
| `background` | string | `#000000` | Padding color for `contain`, `#rgb` or `#rrggbb`. |
| `rotation` | int | 0 | Extra clockwise rotation: 0, 90, 180 or 270. |
| `mirror` | bool | false | Horizontal flip. |
| `start` | number | 0 | Seconds skipped at the start of the source. Must be before the end. |
| `duration` | number | 0 (to the end) | Longest stretch of source to use. |
| `speed` | number | 1 | 0.25..4. Audio tempo follows. |
| `bw` | bool | false | Grayscale. |
| `brightness` | number or null | null (unchanged) | -1..1, neutral 0. |
| `contrast` | number or null | null | 0..2, neutral 1. |
| `saturation` | number or null | null | 0..3, neutral 1. |
| `sharpness` | number or null | null | 0..2, neutral 0 (unsharp). |
| `blur` | number | 0 | Gaussian sigma, 0..50. |
| `sound` | string | none | Url of a replacement or background audio track. Same url rules as `input`. |
| `no_sound` | bool | false | Remove all audio. Overrides every other audio field, and `sound` is not even downloaded. |
| `sound_start` | number | 0 | Offset into `sound`. Must be before its end. |
| `sound_loop` | bool | false | Loop `sound` until the video ends. |
| `sound_volume` | number or null | 1 | 0..4. 0 mutes. |
| `original_volume` | number or null | 0 with `sound`, else 1 | 0..4. 0 mutes. |
| `normalize` | bool | false | Normalize the final mix to `lufs` and `true_peak`. |
| `lufs` | number | `BEE_VIDEO_LUFS` (-16) | -70..-5. 0 uses the default. |
| `true_peak` | number | `BEE_VIDEO_TRUE_PEAK` (-1.5) | -9..0 dBTP. 0 uses the default. |
| `fade_in` | number | 0 | Audio fade-in, 0..`BEE_VIDEO_MAX_DURATION`. |
| `fade_out` | number | 0 | Audio fade-out, 0..`BEE_VIDEO_MAX_DURATION`. |
| `watermark` | Watermark | none | Overlay on every rendition. |

### Watermark

| json | type | default | rule |
|---|---|---|---|
| `input` | string | required | Url of a png, jpeg or webp, no larger than `BEE_VIDEO_MAX_DIMENSION` on a side. PNG with transparency is recommended. |
| `position` | string | `bottom-right` | A gravity value. |
| `width` | int | `BEE_VIDEO_WATERMARK_WIDTH` (160) | 1..`BEE_VIDEO_MAX_DIMENSION`. Never wider than the output frame. |
| `margin` | int | 0 | Pixels from the chosen edge, 0..`BEE_VIDEO_MAX_DIMENSION`. |
| `opacity` | number or null | 1 | 0..1. |

## Profile

| json | type | default | rule |
|---|---|---|---|
| `name` | string | required for Stream | 1..32 characters of letters, digits, `-`, `_` and `.`, not starting with `.`. Used as the rendition folder. |
| `width`, `height` | int | required | Even, positive, at most `BEE_VIDEO_MAX_DIMENSION` (3840). |
| `fps` | int | 0 | 0..120. Required for Stream renditions. 0 keeps the source rate (Video and preview only). |
| `video_codec` | string | `h264` | `h264` (libx264), `hevc` (libx265) or `av1` (libsvtav1). |
| `pixel_format` | string | `yuv420p` | `yuv420p`, or `yuv420p10le` (hevc and av1 only). |
| `preset` | string | encoder default | h264/hevc: `ultrafast` … `veryslow` (no `placebo`). av1: `0`..`13`. |
| `rate_control` | string | `vbr` | `vbr` or `crf`. |
| `video_bitrate` | int | required for vbr | 0..100000 kbit/s. A target, not a size guarantee. |
| `max_bitrate` | int | 0 | 0..200000. Needs `buffer_size`, and must not be below `video_bitrate` for vbr. |
| `buffer_size` | int | 0 | 0..400000 kbit. Needs `max_bitrate`. |
| `crf` | int | 0 | Used for `crf` only. 0..51 (0..63 for av1). Always present in json. |
| `sound_codec` | string | `aac` | Only `aac`. |
| `sound_bitrate` | int | `BEE_VIDEO_SOUND_BITRATE` (128) | 16..512. |
| `sample_rate` | int | 48000 | 44100 or 48000. |
| `channels` | int | 2 | 1 or 2. |

## HLS (Stream only)

| json | type | default | rule |
|---|---|---|---|
| `format` | string | `fmp4` | `fmp4`, or `mpegts` (h264 only). |
| `segment_duration` | number | 2 | 1..10 seconds. Must be a whole number of `keyframe` intervals. |
| `keyframe` | number | `segment_duration` | Above 0 and at most `segment_duration`. `fps × keyframe` must be a whole number of frames for every profile. |
| `key` | bytes (base64) | none | AES-128 key, exactly 16 bytes. |
| `key_uri` | string | with `key` | Key address for the player, e.g. `skd://1111`. Printable ASCII, no spaces or quotes, at most 1024 bytes. Not allowed without `key`. |

## Thumbnail

A still taken from the processed timeline, after `speed`.

| json | type | default | rule |
|---|---|---|---|
| `output` | string | required | Key whose extension matches the format: `.jpg`, `.png`, `.webp` or `.avif`. |
| `position` | number | 0 (first frame) | Seconds on the output timeline. Past the end, the last frame is used. |
| `width`, `height` | int | 0 | Bounding box, 0..`BEE_VIDEO_MAX_DIMENSION`. 0 means the side of the output frame. The aspect ratio is kept, and the result is never larger than the frame unless `upscale`. |
| `format` | string | `jpg` | `jpg`, `png`, `webp` or `avif`. |
| `quality` | int | `BEE_VIDEO_IMAGE_QUALITY` (80) | 1..100. |

## Preview

A short MP4 cut from the processed timeline, after `speed`. It is never
encrypted, even for an encrypted Stream.

| json | type | default | rule |
|---|---|---|---|
| `output` | string | required | Key ending in `.mp4`. |
| `start` | number | 0 | Output timeline offset. Must be before the end. |
| `duration` | number | 3 | Above 0 and up to `BEE_VIDEO_MAX_DURATION`. Cut at the end of the timeline. |
| `profile` | Profile | required | Without `upscale`, it shrinks to the output frame. |
| `sound` | bool | false | Include the final audio mix. |

## Keys

Every output key (`playlist_path` or `file_path`, thumbnail and preview outputs) has one leading
`/` trimmed. After that it must be a clean relative key: no `..`, no
spaces, quotes, `?`, `#` or `\`. It must have the right extension, and keys
must not repeat. With several renditions, no other key may lie in a
rendition folder (`<dir of output>/<name>/`). With one rendition, no other
key next to `playlist_path` may be named `init.mp4` or `segment_*`. A broken key
gives `INVALID_PATH`.

## Presets (Go)

`presets.go` has helpers for vertical 9:16 short video:

- `DefaultHLS()` returns `{format: fmp4, segment_duration: 2, keyframe: 2}`.
- `VerticalOptions()` returns `{vertical: true, resize: cover, gravity: center}`.
- `Preset(q)` returns a fresh `Profile` for one of `240p`, `360p`, `480p`,
  `540p`, `720p`, `1080p` and `1080p60`. Each is h264, yuv420p, preset
  `fast`, vbr, `max_bitrate` = 1.5 × `video_bitrate`, `buffer_size` = 2 ×
  `max_bitrate`, aac 48 kHz stereo, 30 fps (60 for `1080p60`).
- Ladders: `VerticalMobile()` (360p, 720p), `VerticalStandard()` (360p,
  720p, 1080p), `VerticalEconomy()` (240p, 480p, 720p), `VerticalFull()`
  (240p, 360p, 480p, 720p, 1080p).
- `Profile.EstimatedBytes(duration, withAudio)` estimates the payload at the
  target bitrates. It is not a limit.

| preset | size | video kbit/s | sound kbit/s |
|---|---|---|---|
| 240p | 240×426 | 250 | 64 |
| 360p | 360×640 | 500 | 96 |
| 480p | 480×854 | 900 | 96 |
| 540p | 540×960 | 1200 | 96 |
| 720p | 720×1280 | 1800 | 128 |
| 1080p | 1080×1920 | 3500 | 128 |
| 1080p60 | 1080×1920 | 6000 | 192 |

Outside Go, write the profile out. `Preset("720p")` is:

```json
{"name": "720p", "width": 720, "height": 1280, "fps": 30, "video_codec": "h264", "pixel_format": "yuv420p",
 "preset": "fast", "rate_control": "vbr", "video_bitrate": 1800, "max_bitrate": 2700, "buffer_size": 5400,
 "crf": 0, "sound_codec": "aac", "sound_bitrate": 128, "sample_rate": 48000, "channels": 2}
```

## Example Stream job

```json
{
  "id": 1111,
  "meta": {"user": 7},
  "input": "https://uploads.example.com/raw/1111.mov?X-Amz-Signature=...",
  "playlist_path": "video/1111/index.m3u8",
  "options": {"vertical": true, "resize": "cover", "gravity": "center", "normalize": true},
  "profiles": [
    {"name": "360p", "width": 360, "height": 640, "fps": 30, "preset": "fast", "video_bitrate": 500, "max_bitrate": 750, "buffer_size": 1500, "crf": 0, "sound_bitrate": 96},
    {"name": "720p", "width": 720, "height": 1280, "fps": 30, "preset": "fast", "video_bitrate": 1800, "max_bitrate": 2700, "buffer_size": 5400, "crf": 0, "sound_bitrate": 128}
  ],
  "hls": {"format": "fmp4", "segment_duration": 2, "keyframe": 2, "key": "MDEyMzQ1Njc4OWFiY2RlZg==", "key_uri": "skd://1111"},
  "thumbnails": [{"output": "video/1111/cover.webp", "position": 1.5, "width": 360, "format": "webp"}],
  "preview": {"output": "video/1111/preview.mp4", "duration": 3,
    "profile": {"width": 360, "height": 640, "fps": 30, "video_bitrate": 500, "crf": 0}}
}
```

## Example Video job

```json
{
  "id": "clip-1112",
  "meta": {"user": 7},
  "input": "https://uploads.example.com/raw/1112.mp4?X-Amz-Signature=...",
  "file_path": "video/1112/video.mp4",
  "options": {"start": 2, "duration": 30, "watermark": {"input": "https://cdn.example.com/logo.png", "margin": 24}},
  "profile": {"name": "720p", "width": 720, "height": 1280, "fps": 30, "video_bitrate": 1800, "max_bitrate": 2700, "buffer_size": 5400, "crf": 0},
  "thumbnails": [{"output": "video/1112/cover.jpg"}]
}
```

## Pipeline

1. Validate and fill the defaults (`videocheck.go`). Check that this
   ffmpeg build has the encoders the job needs, or fail with
   `UNSUPPORTED_CODEC`.
2. Take a slot and start `BEE_VIDEO_TIMEOUT`. Existing objects at
   the keys are not checked. A NATS redelivery runs the job again.
3. Download the source, `options.sound` and the watermark. All downloads
   of a job share `BEE_DOWNLOAD_TIMEOUT`, and each file is limited to
   `BEE_VIDEO_MAX_SOURCE_MB`.
4. Probe each download:
   - Source codecs: h264, hevc, av1, vp8, vp9, mpeg4, mpeg2video and
     prores. Any other gives `UNSUPPORTED_FORMAT`; no video stream gives
     `INVALID_VIDEO`.
   - Duration must be within `BEE_VIDEO_MIN_DURATION`..`BEE_VIDEO_MAX_DURATION`,
     and each side at most `BEE_VIDEO_MAX_DIMENSION`.
   - HDR is converted to SDR.
   - Sound codecs: pcm, aac, mp3, opus, vorbis, flac, alac, ac3 and eac3
     (else `INVALID_SOUND`).
   - Watermark: png, jpeg or webp (else `INVALID_WATERMARK`).
5. Plan the job. The output timeline is
   `min(source − start, duration) / speed`, and it must also fit within the
   min and max duration.
6. Run the loudness pass (for `normalize`), then one ffmpeg run for every
   output. Each output is then checked (stream, size, duration, segments),
   or the job fails with `VERIFY_FAILED`.
7. Upload in waves with the entry object last, then build the result.

### No upscale

Without `options.upscale`, the source is never enlarged (1 % rounding is
allowed):

- **Stream:** a profile larger than the source is skipped. It is not in
  the master playlist and not in `result.profiles`. If every profile is
  skipped, the job fails with `NO_RENDITION`.
- **Video:** a profile larger than the source fails the job with
  `NO_RENDITION`.
- **Thumbnails and preview:** they shrink to the output frame.

The output frame (canvas) is the source frame, after rotation, fitted into
the largest remaining profile.

## Storage layout

**Stream with several renditions**, for `"playlist_path": "video/1111/index.m3u8"`:

```
video/1111/index.m3u8                 master playlist (output)
video/1111/360p/index.m3u8            media playlist
video/1111/360p/init.mp4              fmp4 only, never encrypted
video/1111/360p/segment_00000.m4s     segment_NNNNN.m4s (fmp4) or .ts (mpegts)
video/1111/720p/...
video/1111/cover.webp                 thumbnail at its key
video/1111/preview.mp4                preview at its key
```

The master lists `<name>/index.m3u8` with `BANDWIDTH` (peak) and
`AVERAGE-BANDWIDTH`, both measured from the published segments, plus
resolution, frame rate and codecs.

**Stream with one rendition:** `playlist_path` is the media playlist itself.
`init.mp4` and `segment_NNNNN.*` sit next to it in the same folder, and
there is no master.

**Video:** the MP4 is at `file_path`.

**Thumbnails and preview** are always at their own keys.

### Encryption (Stream)

With `hls.key`, every segment of every rendition is encrypted whole with
AES-128-CBC and PKCS7 padding, using a new random IV per segment. Each
media playlist gets
`#EXT-X-KEY:METHOD=AES-128,URI="<key_uri>",IV=0x<hex>` above each
`#EXTINF`. `init.mp4`, the master, thumbnails and preview are not
encrypted. The key is never stored or logged.

### Ordering guarantees

1. `init.mp4` and segments (encrypted on the fly), thumbnails, preview.
2. The media playlists, when there are several renditions. They need the
   IVs from wave 1.
3. Bee checks that every object above is in storage (each media playlist,
   init, the first and last segment, thumbnails, preview).
4. The entry object: master, the single media playlist, or the MP4. Then
   it is checked.

An entry in storage means everything it references is in place. A job that
fails earlier leaves no entry, so it can be rerun with the same keys. Its
leftover objects get overwritten.

### Existing keys

The same as for audio ([jobs-audio.md](jobs-audio.md#existing-keys)):
every job that arrives runs and writes over whatever lies at its keys;
taken keys are your concern. A NATS redelivery runs the job again.

## Result

| json | type | meaning |
|---|---|---|
| `id` | string or number | Job id, exactly as sent. |
| `meta` | object | Job `meta`, exactly as sent. |
| `elapsed` | int | Milliseconds of the whole job. |
| `sha256` | string | Of the source file. |
| `source_size` | int | Source bytes. |
| `playlist_path` | string | Stream: the entry playlist key, as saved. |
| `file_path` | string | Video: the MP4 key, as saved. |
| `duration` | number | Processed duration, seconds. |
| `size` | int | Bytes published in total: renditions, master, thumbnails and preview. |
| `profiles` | ProfileResult[] | Published renditions in order (skipped ones are left out). |
| `thumbnails` | ImageResult[] | In job order. |
| `preview` | ProfileResult | If asked. |
| `error` | `{code, message, retryable}` | Failure only. A failed result has only `id`, `meta`, `elapsed` and `error`. |

**ProfileResult:** `name`, `output` (media playlist key, the mp4 key, or the
preview key), `duration`, `width`, `height`, `fps`, `video_codec`,
`video_bitrate` (measured average kbit/s), `sound_codec`, `sound_bitrate`
(only with sound), `has_sound`, `size` (bytes of the rendition: playlist,
init and segments, or the file), `segments` (0 for MP4).

**ImageResult:** `output`, `width`, `height`, `format`, `size`.

### Example result

```json
{
  "id": 1111,
  "meta": {"user": 7},
  "elapsed": 21834,
  "sha256": "5b1d…a0",
  "source_size": 48211934,
  "playlist_path": "video/1111/index.m3u8",
  "duration": 14.5,
  "size": 5127336,
  "profiles": [
    {"name": "360p", "output": "video/1111/360p/index.m3u8", "duration": 14.5, "width": 360, "height": 640, "fps": 30,
     "video_codec": "h264", "video_bitrate": 487, "sound_codec": "aac", "sound_bitrate": 95, "has_sound": true, "size": 1098412, "segments": 8},
    {"name": "720p", "output": "video/1111/720p/index.m3u8", "duration": 14.5, "width": 720, "height": 1280, "fps": 30,
     "video_codec": "h264", "video_bitrate": 1764, "sound_codec": "aac", "sound_bitrate": 127, "has_sound": true, "size": 3501330, "segments": 8}
  ],
  "thumbnails": [{"output": "video/1111/cover.webp", "width": 360, "height": 640, "format": "webp", "size": 18734}],
  "preview": {"name": "", "output": "video/1111/preview.mp4", "duration": 3, "width": 360, "height": 640, "fps": 30,
    "video_codec": "h264", "video_bitrate": 498, "has_sound": false, "size": 508211, "segments": 0}
}
```

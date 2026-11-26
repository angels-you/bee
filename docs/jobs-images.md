# Image jobs

An image job takes one original by url and makes any number of results from
it. Each task is one output file: resized, cropped or padded, with optional
effects, encoded as jpg, png, webp or avif, and optionally kept under a
byte limit. The original is decoded once and every task reuses it.

- Kind: `images`. HTTP `POST /v1/images`, gRPC `bee.v1.Bee/Images`, NATS
  `{"kind":"images","job":{...}}` on the jobs subject. See [transports.md](transports.md).
- Go types: `github.com/angels-you/bee/pkg/types/images` (`Job`, `Task`,
  `Profile`, `Result`, `TaskResult`, `Format`, `Fit`, `Geometry`).
- Pipeline: `internal/engine/images.go` (vips).
- Error codes: [errors.md](errors.md). Env settings: [configuration.md](configuration.md).

## Job

| json | type | required | meaning |
|---|---|---|---|
| `id` | string or number | yes | A string of 1..128 printable characters (any language, spaces allowed, no line breaks) or a whole number. Returned in the result exactly as sent. |
| `meta` | object | no | Your own data. bee never reads it and returns it untouched in the result, on failure too. At most 16 KB of json. |
| `input` | string | yes | Url of the original (presigned GET or public https), logged without its query. Scheme and host rules are checked at download, see [security.md](security.md). |
| `tasks` | Task[] | yes, at least 1 | Results, made in this order. |

## Task

| json | type | required | meaning |
|---|---|---|---|
| `id` | string | yes | Any non-empty string, unique within the job. |
| `name` | string | no | Your label, returned as is. |
| `output` | string | yes | Storage key, unique within the job. One leading `/` is trimmed. The rest must be a clean relative key: no `..`, `//`, spaces, `?`, `#` or `\`. |
| `profile` | Profile | yes | How to make the result. |

Bee does not check that the extension of `output` matches the format. The
object's Content-Type comes from the format (`image/jpeg`, `image/png`,
`image/webp`, `image/avif`), so use matching extensions anyway.

## Profile

| json | type | default | rule |
|---|---|---|---|
| `format` | string | required | `jpg`, `png`, `webp` or `avif`. |
| `fit` | string | `inside` | `inside`, `cover`, `contain` or `fill`, see [Geometry](#geometry). |
| `quality` | int | — | 1..100, required for lossy output (jpg, and webp/avif without `lossless`). Ignored for png and lossless, where it may be 0. |
| `max_width` | int | 0 | Box width, px. 0 is unlimited (inside only). At most `BEE_IMAGES_MAX_DIMENSION`. |
| `max_height` | int | 0 | Box height, px. Same rules as `max_width`. |
| `center` | bool | false | Cover crop: `true` crops from the center, `false` keeps the most interesting area (vips attention). |
| `background_color` | string | `""` | `#rgb`, `#rrggbb` or `#rrggbbaa`. The contain background, and what jpg flattens transparency onto. Empty: a transparent contain background, and black for jpg. |
| `blur` | number | 0 | Gaussian blur, 0..3. 0 is none, 1 = 100 %. |
| `brightness` | number | 0 | Color channel multiplier, 0..3. 0 or 1 means no change, and 1.5 = 150 %. Alpha is unchanged. |
| `sharpness` | number | 0 | Unsharp mask, 0..3. 0 or 1 means no change, 1.5 is sharper, 0.9 is softer. |
| `upscale` | bool | false | The original may be enlarged up to the box. |
| `max_bytes` | int | 0 | Size limit, bytes. 0 is none. Lossy quality steps down toward `min_quality` until the file fits. |
| `min_quality` | int | `BEE_IMAGES_MIN_QUALITY` (30) | 0..100. 0 uses the default. The floor for `max_bytes`. |
| `effort` | int or null | `BEE_IMAGES_AVIF_EFFORT` (4) | Avif encoder effort, 0..9. Higher is slower and smaller. Absent or `null` uses the default, and 0 is a real value. |
| `lossless` | bool | false | Lossless webp or avif. png is always lossless, and jpg cannot be (`INVALID_JOB`). |
| `thumbhash` | bool | false | Return `thumbhash` of the result. |
| `dominant_color` | bool | false | Return `dominant_color`. |
| `palette_colors` | int | 0 | Return up to this many colors in `palette`, 0..16. |
| `average_color` | bool | false | Return `average_color` (weighted by alpha). |

Blur and sharpness radii are fractions of the result's long side, so an
effect looks the same at any size.

Out-of-range values give these errors:

- `quality` and `min_quality`: `INVALID_QUALITY`.
- Negative boxes, a missing box for cover/contain/fill, or a box above the
  limit: `INVALID_DIMENSIONS`.
- Anything else: `INVALID_JOB`.

## Geometry

The original is first rotated by its EXIF orientation. Then, for an
original of `w×h`:

| fit | result |
|---|---|
| `inside` | Fitted into the box with the aspect ratio kept. An axis set to 0 is unlimited. No box at all keeps the original size. |
| `cover` | Exactly the box: covers it and crops the excess (`center` or attention). Without `upscale`, an original smaller than the box is not enlarged, and the result is the largest crop with the box's aspect ratio. |
| `contain` | Exactly the box: the image is fitted as for `inside` and centered, and the rest is filled with `background_color`. |
| `fill` | Exactly the box, stretched. Without `upscale`, each side is at most the original's side. |

Without `upscale`, `inside` and `contain` never enlarge the original. `cover`,
`contain` and `fill` all need `max_width` and `max_height`. A result
side above `BEE_IMAGES_MAX_DIMENSION` fails with `INVALID_DIMENSIONS`,
which can happen with `inside` and no box on a large original.

## max_bytes

Without `max_bytes`, the result is encoded once at `quality`.

With `max_bytes`, the image is prepared once and only the encode is
repeated:

- **Lossy:** first at `quality`. If too big, bee binary-searches between
  `min_quality` and `quality` for the highest quality that fits.
  `TaskResult.quality` is the quality used. If even `min_quality` is too
  big, or `min_quality >= quality`, the job fails with `OUTPUT_TOO_LARGE`.
- **png and lossless:** encoded once. Too big fails with
  `OUTPUT_TOO_LARGE`.

## Limits (server only)

| env | default | on breach |
|---|---|---|
| `BEE_IMAGES_TIMEOUT` | 2m | `INTERNAL_TEMPORARY_ERROR` (job timed out) |
| `BEE_IMAGES_MAX_SOURCE_MB` | 50 | `SOURCE_TOO_LARGE` |
| `BEE_IMAGES_MAX_MEGAPIXELS` | 100 | `PIXEL_LIMIT_EXCEEDED` (after EXIF rotation, width × height) |
| `BEE_IMAGES_MAX_DIMENSION` | 8000 | `INVALID_DIMENSIONS` (box or result side) |
| `BEE_IMAGES_THREADS` | 4 | threads of one vips run |

Accepted originals are jpeg, png, webp, avif, heic, heif, tiff and gif.
Anything else gives `UNSUPPORTED_FORMAT`, and an unreadable file gives
`INVALID_IMAGE`.

## Pipeline

1. Validate the job, apply the box limit, and fill `fit`, `min_quality`
   and `effort` from the defaults.
2. Take a slot, start the timeout, and download the original.
3. Read the header, check the pixel limit, and decode once (EXIF rotation
   applied).
4. Make each task in order. Each result is checked (it reads back in its
   format and size, with no metadata) before the next one starts. The first
   error fails the job, and its message is prefixed with `task <id>:`.
5. Upload all results in one wave (`BEE_UPLOAD_CONCURRENCY` at a time).

Results carry no metadata: no EXIF, XMP, IPTC, ICC or comments.

## Storage

Each result is written to its `output` key, and nothing else is written.

- **Existing objects:** an object already at an `output` key is
  overwritten. bee does not check existing keys.
  Choose keys that do not collide with objects you want to keep; that is
  your concern, not bee's.
- **Failures:** a job that fails before the upload touches nothing in
  storage. A failure during the upload can leave some results written.
  Retrying the same job overwrites them.

## Example job

```json
{
  "id": 77,
  "meta": {"user": 7},
  "input": "https://uploads.example.com/raw/77.heic?X-Amz-Signature=...",
  "tasks": [
    {
      "id": "large",
      "output": "photos/77/large.avif",
      "profile": {"format": "avif", "quality": 60, "max_width": 2048, "max_height": 2048, "max_bytes": 400000, "thumbhash": true, "dominant_color": true}
    },
    {
      "id": "square",
      "name": "avatar",
      "output": "photos/77/square.webp",
      "profile": {"format": "webp", "fit": "cover", "quality": 80, "max_width": 256, "max_height": 256, "palette_colors": 5}
    },
    {
      "id": "card",
      "output": "photos/77/card.jpg",
      "profile": {"format": "jpg", "fit": "contain", "quality": 85, "max_width": 1200, "max_height": 630, "background_color": "#ffffff", "sharpness": 1.2}
    }
  ]
}
```

## Result

| json | type | meaning |
|---|---|---|
| `id` | string or number | Job id, exactly as sent. |
| `meta` | object | Job `meta`, exactly as sent. |
| `tasks` | TaskResult[] | In task order. Absent on failure. |
| `elapsed` | int | Milliseconds for the whole job. |
| `sha256` | string | Of the original. |
| `source_size` | int | Bytes of the original. |
| `source_format` | string | `jpg`, `png`, `webp`, `avif`, `heic`, `heif`, `tiff` or `gif`. |
| `source_width`, `source_height` | int | After EXIF rotation. |
| `error` | `{code, message, retryable}` | Failure only. |

On failure, the source fields are filled as far as the job got: size and
sha256 after the download, and format and size after the header.

**TaskResult:**

| json | meaning |
|---|---|
| `id`, `name` | From the task. |
| `output` | The key as saved, without a leading `/`. |
| `format` | Output format. |
| `width`, `height` | Result size. |
| `size` | Bytes. |
| `quality` | Quality used, lower than asked if `max_bytes` lowered it. 0 for png and lossless. |
| `sha256` | Of the result. |
| `thumbhash` | base64 ThumbHash. Only if asked. |
| `dominant_color` | `#rrggbb`, the most common color. Only if asked. |
| `palette` | `#rrggbb` colors, most common first. Only if asked. |
| `average_color` | `#rrggbb`. Only if asked. |
| `elapsed` | Milliseconds for this result. |

### Example result

```json
{
  "id": 77,
  "meta": {"user": 7},
  "tasks": [
    {"id": "large", "output": "photos/77/large.avif", "format": "avif", "width": 1536, "height": 2048, "size": 398112, "quality": 52,
     "sha256": "c3a1…9f", "thumbhash": "1QcSHQRnh493V4dIh4eXh1h4kJUI", "dominant_color": "#5a6b3c", "elapsed": 2140},
    {"id": "square", "name": "avatar", "output": "photos/77/square.webp", "format": "webp", "width": 256, "height": 256, "size": 14876, "quality": 80,
     "sha256": "07be…12", "palette": ["#5a6b3c", "#c9d2b0", "#2b2f1d", "#8f9a6a", "#e8e4d9"], "elapsed": 310},
    {"id": "card", "output": "photos/77/card.jpg", "format": "jpg", "width": 1200, "height": 630, "size": 96233, "quality": 85,
     "sha256": "e41d…6a", "elapsed": 402}
  ],
  "elapsed": 3471,
  "sha256": "8d0f…b7",
  "source_size": 3120544,
  "source_format": "heic",
  "source_width": 3024,
  "source_height": 4032
}
```

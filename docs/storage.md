# Storage

bee downloads the original by the url in the job and writes every output
to **one** storage. One bee is connected to exactly one storage: a local
folder or one S3 bucket, chosen by `BEE_STORAGE`. A job never names a
bucket, an endpoint or credentials; it names only **keys** inside the
storage of the bee it is sent to. To write to two buckets, run two bees
(on NATS, with different subjects, see [configuration.md](configuration.md#nats)).

The code is in [`pkg/storage`](../pkg/storage): the `Storage` interface,
`local`, `s3` and the source downloader.

## Local

```sh
BEE_STORAGE=local
BEE_LOCAL_DIR=/data/out
```

- Keys are files under `BEE_LOCAL_DIR`; folders are created as needed.
  The root folder is created at start if missing.
- Each object is written to `<file>.tmp` and renamed, so a reader never
  sees half a file.
- Content types are not stored; serve the folder with a web server that
  sets them by extension.
- In the docker image `/data/out` is a volume owned by the `bee` user
  (uid 10001). Mount a folder that this uid can write to.

## S3

```sh
BEE_STORAGE=s3
BEE_S3_PROVIDER=cloudflare
BEE_S3_ACCOUNT=<cloudflare account id>
BEE_S3_KEY=...
BEE_S3_SECRET=...
BEE_S3_BUCKET=media
BEE_S3_PREFIX=bee        # optional
```

Any S3-compatible provider works through
[minio-go](https://github.com/minio/minio-go). `BEE_S3_PROVIDER` picks the
default endpoint and region from the provider list
([`pkg/storage/s3/provider/list`](../pkg/storage/s3/provider/list)).

### How the endpoint and region are chosen

- `BEE_S3_REGION` empty: the default region of the provider (table below).
  For `cloudflare` the region is always `auto`, whatever you set.
- `BEE_S3_ENDPOINT` empty: the endpoint template of the provider, with
  `<region>` replaced by the region and `<account>` by `BEE_S3_ACCOUNT`.
  If the template needs `<account>` and `BEE_S3_ACCOUNT` is empty, the start
  fails. Providers without a template (`idrive`, `ceph`) need
  `BEE_S3_ENDPOINT`.
- `BEE_S3_ENDPOINT` set: used as is (host and optional port, no scheme),
  e.g. `fsn1.your-objectstorage.com` or `minio:9000`.
- `BEE_S3_SECURE` (default `true`) selects https. Only `minio` and `ceph`
  honour `false`; every other provider is always https.
- `BEE_S3_PREFIX` is a folder inside the bucket; every key is written under
  it (`<prefix>/<key>`). Empty: the bucket root. The prefix is normalized
  (slashes trimmed, `..` and `//` removed).

### At start

bee checks that the bucket exists and the credentials work (`Ping`, a
"bucket exists" request). bee does not create the bucket. A failure stops
the start with exit code 1. `/ready` on the health server repeats the
check.

### Objects

- Every object is written with
  `Cache-Control: public, max-age=31536000, immutable`. bee always writes
  outputs under keys the client chose for this job; serve them from a CDN
  without revalidation. If you reuse keys (images allow it, see below), a
  CDN in front may keep the old object.
- `Content-Type` is set by extension:

  | Extension | Content-Type |
  |---|---|
  | `.m3u8` | `application/vnd.apple.mpegurl` |
  | `.mp4` | `video/mp4` |
  | `.m4s` | `video/iso.segment` |
  | `.ts` | `video/mp2t` |
  | `.json` | `application/json` |
  | `.jpg`, `.jpeg` | `image/jpeg` |
  | `.png` | `image/png` |
  | `.webp` | `image/webp` |
  | `.avif` | `image/avif` |
  | other | `application/octet-stream` |

  An image output takes the content type of its format.
- bee only writes and checks for existence. It never reads, lists or
  deletes objects, and it does not set ACLs: make the bucket (or a CDN in
  front of it) public if players need to read the files.

### Providers

`BEE_S3_PROVIDER` is the name in the first column (lower case, exact).

| Name | Provider | Default region | Endpoint template |
|---|---|---|---|
| `hetzner` | Hetzner S3 | `hel1` | `<region>.your-objectstorage.com` |
| `selectel` | Selectel S3 | `ru-1` | `s3.storage.selcloud.ru` |
| `cloudflare` | Cloudflare R2 | `auto` (always) | `<account>.r2.cloudflarestorage.com` |
| `backblaze` | Backblaze B2 | `us-west-004` | `s3.<region>.backblazeb2.com` |
| `aws` | Amazon S3 | `us-east-1` | `s3.<region>.amazonaws.com` |
| `gcs` | Google Cloud Storage | `auto` | `storage.googleapis.com` |
| `wasabi` | Wasabi | `us-east-1` | `s3.<region>.wasabisys.com` |
| `digitalocean` | DigitalOcean Spaces | `nyc3` | `<region>.digitaloceanspaces.com` |
| `linode` | Akamai Linode Object Storage | `us-east-1` | `<region>.linodeobjects.com` |
| `vultr` | Vultr Object Storage | `ewr1` | `<region>.vultrobjects.com` |
| `scaleway` | Scaleway Object Storage | `fr-par` | `s3.<region>.scw.cloud` |
| `ovh` | OVHcloud Object Storage | `gra` | `s3.<region>.io.cloud.ovh.net` |
| `upcloud` | UpCloud Object Storage | `europe-1` | `<account>.upcloudobjects.com` |
| `exoscale` | Exoscale SOS | `ch-gva-2` | `sos-<region>.exo.io` |
| `contabo` | Contabo Object Storage | `eu2` | `<region>.contabostorage.com` |
| `ionos` | IONOS S3 Object Storage | `eu-central-1` | `s3.<region>.ionoscloud.com` |
| `ibm` | IBM Cloud Object Storage | `us-south` | `s3.<region>.cloud-object-storage.appdomain.cloud` |
| `oracle` | Oracle Cloud Object Storage | `us-ashburn-1` | `<account>.compat.objectstorage.<region>.oraclecloud.com` |
| `alibaba` | Alibaba Cloud OSS | `oss-cn-hangzhou` | `<region>.aliyuncs.com` |
| `tencent` | Tencent Cloud COS | `ap-guangzhou` | `cos.<region>.myqcloud.com` |
| `huawei` | Huawei Cloud OBS | `cn-north-4` | `obs.<region>.myhuaweicloud.com` |
| `yandex` | Yandex Object Storage | `ru-central1` | `storage.yandexcloud.net` |
| `vk` | VK Cloud Object Storage | `ru-msk` | `hb.<region>.vkcloud-storage.ru` |
| `timeweb` | Timeweb Cloud S3 | `ru-1` | `s3.timeweb.cloud` |
| `storj` | Storj | `us1` | `gateway.storjshare.io` |
| `idrive` | IDrive e2 | none | none: set `BEE_S3_ENDPOINT` (and `BEE_S3_REGION` if needed) |
| `filebase` | Filebase | `us-east-1` | `s3.filebase.com` |
| `tebi` | Tebi | `global` | `s3.tebi.io` |
| `minio` | MinIO (self-hosted) | `us-east-1` | `localhost:9000` |
| `ceph` | Ceph RGW (self-hosted) | `default` | none: set `BEE_S3_ENDPOINT` |

Examples:

```sh
# Hetzner in Falkenstein
BEE_S3_PROVIDER=hetzner
BEE_S3_REGION=fsn1            # endpoint: fsn1.your-objectstorage.com

# MinIO next to bee, plain http
BEE_S3_PROVIDER=minio
BEE_S3_ENDPOINT=minio:9000
BEE_S3_SECURE=false
```

A provider that is not in the list but speaks S3 usually works as `minio`
or `ceph` with `BEE_S3_ENDPOINT` and `BEE_S3_REGION` set.

## Keys

Output keys come from the job (`output`, `wave_path`, `meta_path`, ... and the
`output` of every image task). They are keys relative to the storage root,
not urls and not bucket names. A key must be:

- relative and already clean: no `..`, no `//`, no `.` alone, no `\`;
- without spaces, tabs, line breaks, `?` or `#`;
- one leading `/` is allowed and trimmed: `/media/a.m3u8` is `media/a.m3u8`.

A key that breaks the rules fails the job with `INVALID_PATH` before any
download. Two outputs of one job may not share a key (`INVALID_PATH`).
Each job type also checks extensions (see the job pages:
[audio](jobs-audio.md), [video](jobs-video.md), [images](jobs-images.md)).

With `BEE_S3_PREFIX=bee` the job key `media/a.m3u8` is the object
`bee/media/a.m3u8`.

## Existing keys

### Images: overwrite

An images job writes all its outputs only after every task has been
rendered and checked. It does not look at what is already stored: an
existing object under the same key is overwritten. Choosing keys that do
not collide is up to the client. A retry of the same job simply writes the
same keys again.

### Audio and video: overwrite, entry object last

Audio and video jobs do not look at what is stored either: they write over
whatever lies at their keys. Taken keys are the client's concern.

They have an **entry object**: the master playlist (`output` of audio and
stream) or the MP4 (`output` of video). The entry object is written
**last**, after every segment and playlist, and after bee has checked that
its own files are in storage. Whoever sees the entry sees a complete set.

A job that failed half way may leave segments behind without an entry
object. A retry with the same keys overwrites them and writes the entry
object at the end.

Every job that arrives runs and writes over its keys, on every transport.
bee never checks whether keys are taken. A NATS redelivery (lost ack,
crashed bee) runs the job again. See
[transports.md](transports.md#redelivery).

## Source download

How bee downloads the originals (https only, private addresses refused,
redirects, size limits, host allow-list) is described in
[security.md](security.md#source-urls).

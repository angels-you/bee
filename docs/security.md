# Security

bee runs jobs that come from your services and downloads urls that come in
those jobs. This page lists what bee protects by itself and what is left
to you.

## Job transports

### HTTP and gRPC: BEE_TOKEN

HTTP and gRPC are never open. When `BEE_HTTP_ADDR` or `BEE_GRPC_ADDR` is
set, `BEE_TOKEN` is required; without it bee does not start.

- HTTP: every request needs `Authorization: Bearer <BEE_TOKEN>`. A bad or
  missing token gets HTTP `401` with the body
  `{"error":{"code":"INVALID_JOB","message":"bad or missing token","retryable":false}}`.
- gRPC: every call needs the metadata `authorization: Bearer <BEE_TOKEN>`.
  A bad or missing token gets the status `Unauthenticated`.
- The token is compared in constant time (`crypto/subtle`).
- One token for both transports. Use a long random value, for example
  `openssl rand -hex 32`. Change the `change-me` of `cmd/.env`.

bee serves HTTP and gRPC **without TLS**. The token travels in clear text
unless you put bee in a private network or behind a TLS terminator (a
reverse proxy, a load balancer, a service mesh). The Go gRPC client can
dial with TLS (`grpc.Dial(addr, true, token)`) when such a terminator is in
front. See [clients.md](clients.md).

An HTTP job body is limited to 1 MiB.

### NATS: BEE_NATS_TOKEN

NATS does not use `BEE_TOKEN`. Anyone who can publish to the job subjects
(`<BEE_NATS_JOBS>.<kind>`) can make bee work, and anyone who can read the
result subjects can read results. Protect NATS itself:

- `BEE_NATS_TOKEN` is passed to the server as the NATS auth token; empty
  means no auth.
- Restrict subjects with NATS accounts or permissions if several
  services share one NATS.
- Job messages stay in the JetStream jobs stream until a bee acks them;
  results are kept `BEE_NATS_RESULT_TTL` (default `24h`).

### Health server: no token

The health server on `BEE_HEALTH_ADDR` (default `:8081`) is always on and
has no token: `GET /health`, `GET /ready`, `GET /metrics`. It shows no
keys or tokens, but `/ready` (its error text) and `/metrics` tell about the
bee, its dependencies and its load.
Keep it on a private network: do not publish port 8081 to the internet;
let only your orchestrator or Prometheus reach it. The docker
`HEALTHCHECK` (`bee health`) calls it from inside the container.

## Source urls

Every url in a job (the original, a video sound or watermark) is
downloaded by [`pkg/storage/download.go`](../pkg/storage/download.go) with
protection against SSRF (making bee fetch something on your internal
network):

- **https only.** `http://` and other schemes fail with `INVALID_JOB`.
- **No private addresses.** An address is refused when it is loopback,
  private (`10/8`, `172.16/12`, `192.168/16`, `fc00::/7`), link-local
  (`169.254/16`, `fe80::/10`), multicast, unspecified, `0.0.0.0/8` or
  carrier-grade NAT (`100.64.0.0/10`).
  - A host given as an IP literal is checked before the request.
  - A host name is resolved at connect time and **every** resolved address
    is checked: private ones are skipped, bee connects only to a public
    one. If none is public the download fails with `INVALID_JOB`
    ("resolves to a private address"). Because the check happens on the
    connection, a DNS answer that changes between the check and the
    request cannot bypass it.
- **Redirects:** at most 3. Every hop goes through the same scheme, host
  and address checks.
- **No proxy:** proxy environment variables are ignored for downloads.
- **Size limit:** `BEE_AUDIO_MAX_SOURCE_MB`, `BEE_VIDEO_MAX_SOURCE_MB`
  (each file) or `BEE_IMAGES_MAX_SOURCE_MB`. A `Content-Length` above the
  limit is refused before reading; the body is cut at limit + 1 byte in any
  case. Too large: `SOURCE_TOO_LARGE`.
- **Time limit:** all downloads of one job together get
  `BEE_DOWNLOAD_TIMEOUT` (`DOWNLOAD_TIMEOUT`).
- **Host allow-list:** `BEE_SOURCE_HOSTS=r2.cloudflarestorage.com,cdn.example.com`
  allows only these hosts and their subdomains (suffix match on a dot
  boundary, case-insensitive: `example.com` allows `cdn.example.com`, not
  `evilexample.com`). It applies to the first url and every redirect.
  Empty: any public host. Set it in production when your originals always
  come from known places.
- The `User-Agent` is `bee/1`.
- An empty body is `SOURCE_UNAVAILABLE`. A `403` or `400` whose body
  mentions "expire" is `SOURCE_URL_EXPIRED` (an expired presigned url).

`BEE_ALLOW_INSECURE_SOURCE=true` turns off the scheme and address checks
(http and private addresses become allowed). It exists for local
development and tests with a source server on localhost. Never set it on a
bee that takes jobs from outside.

### Presigned urls are not leaked

Source urls are usually presigned: the query string holds the signature.
bee never writes the query (nor userinfo or fragment) to logs, and never
quotes it in errors that go back to the client: urls in logs are cut to
scheme, host and path, and the cause of a download error has its url cut
the same way. Error causes (`Err`) are never sent to clients at all; only
`code` and `message` are.

## HLS encryption

Audio jobs and stream (HLS) video jobs can encrypt their segments with
AES-128 ([`internal/encrypt`](../internal/encrypt)). MP4 video jobs have no
encryption.

- The job carries the key: `key` (exactly 16 bytes, base64 in json) and
  `key_uri`, the address the player uses to fetch the key
  (e.g. `skd://1001` or an https url of your key server). `key_uri` is
  required with `key` and may not contain `"` or line breaks; a bad key or
  uri fails with `INVALID_JOB`. No key: clear HLS.
- Every segment is encrypted whole with AES-128-CBC and PKCS7 padding,
  with a **new random IV per segment** (also across renditions and
  retries). The IV is not in the segment: each media playlist has an
  `#EXT-X-KEY:METHOD=AES-128,URI="<key_uri>",IV=0x<iv>` line before every
  segment.
- The fMP4 init segment (`#EXT-X-MAP`) is not encrypted. The master
  playlist has no key lines.
- bee never logs the key and never writes it to storage, to the result or to
  the ledger. Serving the key to players (with your own auth) is your key
  server's job.
- With NATS the key is part of the job message, so it sits in the JetStream
  jobs stream until the job is acked. Protect NATS accordingly.

This is the HLS AES-128 method, not CMAF CENC/CBCS DRM.

## Container

- The image runs as the user `bee` (uid 10001, no shell, no home), not
  root.
- Only `/tmp/bee` (`BEE_TEMP_DIR`) and `/data/out` (the default local
  storage, a volume) are writable for that user.
- Exposed ports: `8080` (http), `9090` (grpc), `8081` (health). Publish only
  the ones you use; see above for the health port.
- Secrets (`BEE_TOKEN`, `BEE_S3_SECRET`, `BEE_NATS_TOKEN`) come only from
  the environment. The start log shows the storage provider, bucket and
  prefix, never keys or tokens.

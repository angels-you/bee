# Contributing

Thanks for helping with bee.

- Open an issue first for anything bigger than a small fix, so we can agree on the approach.
- Keep the existing code style: small steps, a short comment above each step, no package-level helpers, `switch` over joined conditions.
- Run the checks before a pull request: `go build ./...`, `go vet ./...`, `go test ./...`. The tests need ffmpeg, ffprobe, vips and vipsheader (with heif); the NATS tests need `nats-server` in PATH. See [docs/development.md](docs/development.md).
- Update the docs and `cmd/.env` when you change a job field, a config key or a transport.
- Comments, messages and docs are in English.

By contributing you agree that your contribution is licensed under the MIT license of this repository.

# Contributing to BotOptimizer

Thank you for helping improve [BotOptimizer](https://github.com/southline-tech/botmemory). This guide covers local setup, tests, and how to open a pull request.

## Prerequisites

- **Go** — use the version in [`go.mod`](go.mod) (currently Go 1.27.3). You need `gofmt`, `go vet`, and `go test` on your PATH.
- **Optional (live Drive backend):** Google OAuth credentials with the `drive.file` scope. Unit tests and local hacking work fine with `BOTOPTIMIZER_BACKEND=memory`; you only need OAuth when exercising the real Drive backend or running `scripts/smoke_drive.py`.

## Dev setup

```sh
git clone https://github.com/southline-tech/botmemory.git
cd botmemory
cp .env.example .env   # fill BOTOPTIMIZER_*; never commit .env
go build -o botoptimizer ./cmd/botoptimizer
```

Environment variables use the **`BOTOPTIMIZER_*`** prefix.

## Build and test

These match [CI](.github/workflows/ci.yml) and the [README](README.md):

```sh
gofmt -l .          # must print nothing
go vet ./...
go test -race ./...
```

On a shared machine, run one package at a time:

```sh
for p in $(go list ./...); do go test -count=1 -race "$p"; done
```

[`scripts/setup_box.sh`](scripts/setup_box.sh) installs Go from `go.mod` if needed, builds `~/.local/bin/botoptimizer`, and checks required env vars **by name only** (never prints secret values). Use `scripts/setup_box.sh --test` to run the full test loop above after the build.

## Local serve

- Default listen address: `BOTOPTIMIZER_ADDR=127.0.0.1:8787` (loopback only).
- MCP endpoint: `POST http://127.0.0.1:8787/mcp` with `Authorization: Bearer <bot token>`.
- **No Drive:** set `BOTOPTIMIZER_BACKEND=memory` (data is lost when the process exits).
- **Drive backend:** set `BOTOPTIMIZER_DRIVE_ROOT_ID` (run `botoptimizer bootstrap` once to create or find the vault folder), plus `BOTOPTIMIZER_OAUTH_CLIENT_ID`, `BOTOPTIMIZER_OAUTH_CLIENT_SECRET`, and `BOTOPTIMIZER_OAUTH_REFRESH_TOKEN`.

Never commit `.env`, OAuth refresh tokens, bearer tokens, or real Drive root ids.

## Pull request conventions

- Branch from `main`; keep each PR focused on one change.
- **Title:** short imperative summary (e.g. `Add bootstrap test-folder flag`).
- **Body:** what changed and why; link a Linear `RAD-` issue when applicable.
- CI must pass: `gofmt`, `go vet`, `go test -race` (see [`.github/workflows/ci.yml`](.github/workflows/ci.yml)).
- Do not commit secrets, tokens, `.env`, or production vault identifiers.

## Developer Certificate of Origin (DCO)

<!-- DECISION: DCO -->

By contributing, you agree that your contributions are licensed under the [Apache License 2.0](LICENSE) (see also [NOTICE](NOTICE)).

**Default:** sign off each commit with the [Developer Certificate of Origin](https://developercertificate.org/):

```sh
git commit -s -m "Your message"
```

That adds a `Signed-off-by:` line to the commit message. If project policy changes to a different grant (for example PR-body-only), update this section and the comment above.

## License and security expectations

BotOptimizer Memory stores **shared notes in Google Drive** (ordinary files/Docs). Anyone with access to the vault folder can read those notes. It is **not** a secrets store — do not put passwords, API keys, OAuth tokens, or similar material in BotOptimizer Memory records. See [SECURITY.md](SECURITY.md) for the threat model and how to report vulnerabilities.

## See also

- [README.md](README.md) — product overview and operator notes
- [LICENSE](LICENSE) · [NOTICE](NOTICE)
- [SECURITY.md](SECURITY.md) — supported versions, private reporting, threat model

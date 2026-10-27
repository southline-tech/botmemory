# botVault

Durable, shared, attributable project-state store for Brett's bots, exposed as an MCP server and backed by Google Drive.

Tracking: Linear RAD-280. Data lives in Drive, never in this repo. No secrets or tokens in git.

## M1 (this branch)

MCP tools over Streamable HTTP at `POST /mcp`: `list_projects`, `list`, `get`, `put` (with `if_version`), `append`, `delete` (soft: archives only, owners only).

- **Auth:** `Authorization: Bearer <bot token>`. `_acl.json` (vault root in Drive) stores only SHA-256 token hashes plus per-project roles (`read` < `write` < `owner`). Deny by default: a token sees only projects explicitly granted; no wildcard. Mint with `botvault mint-token BOT_ID`. Revoke by setting `"revoked": true`; reloaded every 60s.
- **Attribution:** `created_by`/`updated_by` come from the token, never from arguments (unknown args are rejected).
- **Records:** keys are unique per project (`projects/<p>/records/<key>.json`); `kind` is optional metadata and a list filter.
- **Concurrency:** per-key lock; `put` without `if_version` only creates (existing key → `CONFLICT`); updates need `if_version` = current version. Every write bumps `version` and copies the prior version to `projects/<p>/history/<key>/vN.json`.
- **Storage:** `store.Backend` interface; `internal/drive` (Drive v3 REST, drive.file) and `store.Memory` (tests/dev).

```sh
cp .env.example .env   # fill in; .env is gitignored
go build -o botvault ./cmd/botvault && ./botvault serve
for p in acl store vault mcp drive; do go test -count=1 -race ./internal/$p/; done   # one suite at a time
```

### Safety notes
- Binds loopback only (`BOTVAULT_ADDR` must be 127.0.0.1/::1/localhost); Origin must be loopback if sent; `Authorization: Bearer` required.
- Single instance: exclusive flock on `BOTVAULT_LOCK_FILE`. Record writes are also conditional on the storage revision (Drive file `version`), so a stale write from anywhere fails with `CONFLICT`.
- Save order: history/vN (idempotent copy of immutable vN) then the record (commit point). A failure between leaves the record at vN and a retry is safe.
- ACL reloads every 60s; after 5 consecutive failures it fails closed (denies all) and logs loudly. Duplicate token hashes and invalid bot ids are rejected.
- Drive error details are logged server-side only; bots see `drive: upstream error (HTTP n)`.
- `append` to a missing key returns `NOT_FOUND` (create it with `put` first).

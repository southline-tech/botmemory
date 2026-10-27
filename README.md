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
- Single instance: exclusive flock on a lock file fixed per vault, `/var/tmp/botvault-<sha256(BOTVAULT_DRIVE_ROOT_ID)[:12]>.lock` (override with `BOTVAULT_LOCK_FILE`; path logged at startup). Record writes are also conditional on the Drive file `version`, but Drive has no atomic compare-and-swap, so **the concurrency guard only holds for one server process on one host**. Never run two servers against the same Drive root from different hosts (or with different lock-file overrides).
- `BOTVAULT_ADDR=localhost:…` is bound as 127.0.0.1 (never resolved).
- Updates that omit `kind` keep the previous kind.
- Save order: history/vN (idempotent copy of immutable vN) then the record (commit point). A failure between leaves the record at vN and a retry is safe.
- ACL reloads every 60s; after 5 consecutive failures it fails closed (denies all) and logs loudly. **Caveat:** while `_acl.json` is broken or unreadable the last good ACL stays in force for up to ~5 minutes (5 × 60s), so a token revoked in that window keeps working until fail-closed kicks in. Revoke by editing a valid `_acl.json`. Duplicate token hashes and invalid bot ids are rejected.
- Drive error details are logged server-side only; bots see `drive: upstream error (HTTP n)`.
- `append` to a missing key returns `NOT_FOUND` (create it with `put` first).

### Data migration note
No real Drive data exists in any earlier layout (e.g. kind-scoped `records/<kind>/<key>.json`): no real Drive credential has ever been configured for botVault, so all testing has used the fake Drive and memory backends. No migration is needed.

## M2
- Tools: `search` (all terms, case-insensitive, over key/kind/title/tags/first 500 chars; `project:"*"` = every readable project; archived hidden unless `status:"archived"`; limit default 50, max 200), `history` (versions newest first; fetch one with `get` + `version`), `reindex` (owner).
- Derived files per project: `_index.json` (per-key summary; rebuilt automatically if missing/corrupt) and `log.jsonl` (one line per put/append/delete). They are updated after the record commits; a failure there is logged, never fails the write, and `reindex` repairs the index.
- Drive retries 429/5xx/transport errors: 5 attempts, exponential backoff (500ms base, 16s cap) with full jitter, honours `Retry-After`.
- `botvault bootstrap`: with `BOTVAULT_DRIVE_ROOT_ID` unset, finds or creates `botVault` in My Drive (drive.file), seeds a deny-all `_acl.json`, and prints `BOTVAULT_DRIVE_ROOT_ID=<id>`. Idempotent.

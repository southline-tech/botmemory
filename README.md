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
for p in $(go list ./...); do go test -count=1 -race "$p"; done   # every package (cmd/botvault, acl, acladmin, drive, instance, mcp, store, vault), one at a time
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
- Derived files per project: `_index.json` (per-key summary) and `log.jsonl` (one line per put/append/delete). They are updated after the record commits; a failure there is logged and never fails the write. Before each record commit a durable `projects/<p>/_index.dirty` marker is written; it is removed once the index is saved. If the marker exists (including after a restart or crash), the next index access (search or write) rebuilds the index from records, saves it, and removes the marker; a missing or corrupt index is likewise rebuilt and saved. Only unparseable records are skipped (logged, and listed in `reindex`'s `skipped`); any other read error (Drive 5xx/429 after retries, a file over the read cap, a deadline) aborts the rebuild without saving and leaves the index dirty, and one bad project never fails a `project:"*"` search. `reindex` forces a rebuild.
- `log.jsonl` rotates into `log-0001.jsonl`, `log-0002.jsonl`, … before it would pass 8 MiB. No Drive read is ever silently truncated: a file over the 32 MiB read cap is an error.
- Drive retries 429/5xx/transport errors: 5 attempts, exponential backoff (500ms base, 16s cap) with full jitter, honours `Retry-After`, with a 30s cap on total backoff sleep per Drive request. Each tool call (which may make several Drive requests) has a 50s deadline, inside the 60s HTTP WriteTimeout.
- Folder creates are never blindly re-POSTed: each attempt is a single POST, the folder is looked up again before any retry and after every create, and the oldest copy is adopted. So a lost create response cannot leave a duplicate folder, and any duplicate resolves the same way after a restart. This also covers bootstrap's root folder.
- `botvault bootstrap`: with `BOTVAULT_DRIVE_ROOT_ID` unset, finds or creates `botVault` in My Drive (drive.file), seeds a deny-all `_acl.json`, and prints `BOTVAULT_DRIVE_ROOT_ID=<id>`. Idempotent.

### Real-Drive smoke test
`scripts/smoke_drive.py` runs every tool against a live server and prints PASS/FAIL per check. It reads only env vars and never prints the token:

```sh
botvault mint-token smoke-bot         # add the hash to _acl.json as owner of a throwaway project
BOTVAULT_DRIVE_ROOT_ID=... botvault serve &   # OAuth env vars as for serve
BOTVAULT_SMOKE_TOKEN=... BOTVAULT_SMOKE_PROJECT=smoke-tmp scripts/smoke_drive.py   # refuses non smoke-* projects
# BOTVAULT_SMOKE_M1=1 limits it to the M1 tools; BOTVAULT_URL overrides the endpoint.
```
The script refuses any project not named `smoke-*` unless `BOTVAULT_SMOKE_TEST_ROOT=1` is set (use that only against a test vault root). It never deletes anything, so clean up by hand afterwards: delete `projects/<project>/` in the vault's Drive folder, remove the smoke bot from `_acl.json` (back to `{"bots":{}}` if it was the only one), and discard the token.

## M3

### ACL admin CLI
Edits `_acl.json` in the configured backend (same env as `serve`). Every edit is a conditional write on the current Drive version, retried up to 5 times on a concurrent edit:

```sh
botvault acl grant <bot> <project> <role> --token-out FILE [--rotate]   # role: read|write|owner
botvault acl revoke <bot> [--force]                                    # whole bot, every project
botvault acl remove <bot> [--force]                                    # delete the entry entirely (same last-owner rule)
botvault acl list                                                       # bots, roles, active/REVOKED; never hashes
```
- A new bot (or `--rotate`) gets a fresh token. Only its SHA-256 goes into `_acl.json`, and the raw token is written only to `FILE`, which is created with mode 0600 and must not already exist (symlinks are refused). The CLI never prints the token. If the ACL write fails, `FILE` is removed.
- If the ACL write itself fails (Drive may have saved it, or may apply it later), grant re-reads the ACL. If the new hash is there, the grant succeeded. Otherwise the token file is **kept** and grant reports the outcome as unknown, giving the file path. A token whose hash isn't in the ACL can't authenticate, so keeping it is harmless: check `botvault acl list` later and delete the file if the bot isn't granted. Errors where nothing can have been written (validation, refusals, persistent conflicts) remove the file.
- After a write is acknowledged, grant and revoke re-read `_acl.json` and report success only if the change is visible (the hash and role are present, or the bot shows revoked). Otherwise they fail with "change not visible on re-read".
- `botvault acl` runs hold an exclusive lock, `<dir>/acl-<sha256(root id)[:12]>.lock` where `<dir>` is `$XDG_RUNTIME_DIR/botvault`, else `os.UserCacheDir()/botvault` (e.g. `~/.cache/botvault`); the dir is created 0700 and refused unless it is a real directory owned by you with no group/other bits. Override with `BOTVAULT_ACL_LOCK_FILE`; the lock is opened with O_NOFOLLOW and without O_TRUNC, and an existing non-empty file that isn't a botvault lock (a PID line) is refused, never emptied. A second concurrent run is refused, not queued. The lock covers **one host only**: edits from other hosts rely on the Drive version check and the re-read verification. The server only ever reads `_acl.json`, so it doesn't take this lock.
- Token files are fsynced. A failed write or close removes the partial file. `O_NOFOLLOW` covers only the last path component, so keep token files in a directory you own.
- The mint decision is re-checked inside the conditional edit: a bot created concurrently without `--rotate` fails the grant.
- Granting to an existing bot adds or changes the role and keeps its token. A revoked bot can only be re-enabled with `--rotate`.
- `revoke` disables the bot's token for **every** project and keeps its roles. It refuses to revoke the last active owner of any project unless `--force` is given.
- No wildcard projects. `serve` picks changes up within 60s.
- With `BOTVAULT_BACKEND=memory` the CLI edits the CLI process's own memory, which is only useful in tests. With `BOTVAULT_ACL_FILE` set, it refuses (edit that local file by hand).

### Rebuilding after a box wipe
`scripts/setup_box.sh [--test]` installs the Go version from `go.mod` if missing (sha256 checked against go.dev), builds `~/.local/bin/botvault`, and optionally runs every package's tests one at a time. It loads `.env` only if it is a regular file owned by you with mode 600 or 400 and reports the required env vars **by name only** (set/MISSING). It also checks the Drive root id against `~/.config/botvault/drive_root_id`, writing that file from `BOTVAULT_DRIVE_ROOT_ID` if missing and refusing if they differ. The script contains no secrets: restore `.env` and the OAuth refresh token yourself.

### Hardening
- The lock file is opened with `O_NOFOLLOW`, so a symlink planted at the lock path is refused, and an existing non-lock file is refused rather than truncated. The ACL lock lives in a per-user 0700 dir; the server instance lock still defaults to `/var/tmp` (protected by O_NOFOLLOW and the non-lock check).
- CI: `persist-credentials: false` on checkout, and a 15-minute job timeout.

### verifychain-ai pilot
See [docs/pilot-verifychain.md](docs/pilot-verifychain.md) and `scripts/pilot_verifychain.sh` (dry run by default; real Drive grants need an explicit opt-in and approval).

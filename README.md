# botVault

Durable, shared, attributable project-state store for Brett's bots, exposed as an MCP server and backed by Google Drive.

Tracking: Linear RAD-280. Data lives in Drive, never in this repo. No secrets or tokens in git.

## M1 (this branch)

MCP tools over Streamable HTTP at `POST /mcp`: `list_projects`, `list`, `get`, `put` (with `if_version`), `append`, `delete` (soft: archives only, owners only).

- **Auth:** `Authorization: Bearer <bot token>`. `_acl.json` (vault root in Drive) stores only SHA-256 token hashes plus per-project roles (`read` < `write` < `owner`; `"*"` = default). Mint with `botvault mint-token BOT_ID`. Revoke by setting `"revoked": true`; reloaded every 60s.
- **Attribution:** `created_by`/`updated_by` come from the token, never from arguments (unknown args are rejected).
- **Concurrency:** per-key lock; `if_version` 0 = must not exist, N = must currently be N, else `CONFLICT`. Every write bumps `version` and copies the prior version to `history/<kind>/<key>/vN.json`.
- **Storage:** `store.Backend` interface; `internal/drive` (Drive v3 REST, drive.file) and `store.Memory` (tests/dev).

```sh
cp .env.example .env   # fill in; .env is gitignored
go build -o botvault ./cmd/botvault && ./botvault serve
for p in acl store vault mcp drive; do go test -count=1 -race ./internal/$p/; done   # one suite at a time
```

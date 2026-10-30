# botMemory

Shared memory and handoff notes for AI agent fleets — MCP over HTTP, backed by Google Drive. **Not a secrets store:** notes are ordinary Drive files readable to anyone with access to the vault folder.

Also see [AGENTS.md](AGENTS.md) (pasteable bot rules) · [CONTRIBUTING.md](CONTRIBUTING.md) · [SECURITY.md](SECURITY.md)

> **Rename:** formerly **botVault**. Binary `botmemory`, env **`BOTMEMORY_*`** (legacy **`BOTVAULT_*`** still work with a deprecation warning). Default Drive folder `botMemory`; legacy `botVault` folder is adopted by `bootstrap` (same id).

## For agents

### What / when

Use botMemory for **shared, attributable project state** across bots: decisions, specs, handoffs, assignments, and general notes. Do **not** store secrets — content is **Drive-readable**. Threat model: [SECURITY.md](SECURITY.md).

### MCP connection

Streamable HTTP, **`POST`** only, path **`/mcp`**. Default: `http://127.0.0.1:8787/mcp`. Every request needs **`Authorization: Bearer <bot token>`**. Load the token from a mode-`0600` file at launch; never commit or paste tokens (client interpolation varies).

```json
{
  "mcpServers": {
    "botmemory": {
      "url": "http://127.0.0.1:8787/mcp",
      "headers": { "Authorization": "Bearer ${BOTMEMORY_TOKEN}" }
    }
  }
}
```

Protocol: JSON-RPC 2.0 — `initialize` (protocolVersion `2025-06-18`, serverInfo name `botmemory`), `ping`, `tools/list`, `tools/call` (no batch).

### Tools — example `tools/call` arguments

Pass the object below as the tool arguments (exact field names):

| Tool | Example arguments |
|------|-------------------|
| `list_projects` | `{}` |
| `list` | `{"project":"demo"}` |
| `get` | `{"project":"demo","key":"plan"}` |
| `put` (create) | `{"project":"demo","key":"plan","kind":"spec","title":"Plan","body":"..."}` |
| `put` (update) | `{"project":"demo","key":"plan","title":"Plan","if_version":1}` |
| `append` | `{"project":"demo","key":"plan","text":"\n- update"}` |
| `search` | `{"project":"*","query":"handoff rollout"}` |
| `history` | `{"project":"demo","key":"plan"}` |
| `reindex` | `{"project":"demo"}` (owner) |
| `delete` | `{"project":"demo","key":"plan"}` (owner; soft archive) |

**Kinds (closed set):** `decision`, `spec`, `handoff`, `assignment`, `note`.

**Projects / keys:** regex `^[a-z0-9][a-z0-9._-]{0,63}$` — unique key per project; `kind` is metadata, not a namespace.

### Conventions

- Prefer **`handoff`** (or stable keys like `handoff-<topic>`) for turn/context handoffs; include next owner, status, and blockers in the body.
- Use **`append`** for chronological updates without a version check; use **`put`** + **`if_version`** when replacing structured state.
- Use **`search`** with `"project":"*"` only when you need every granted project; otherwise name the project.
- Archived records are hidden from list/search unless you filter with `status:"archived"`.

### Failure modes

- **Server down / connection refused / timeout:** do not retry in a tight loop — report to the human operator once and wait for restart.
- **`401` / token rejected:** stop; do not mint or rotate tokens. Owner checks ACL / runs `botmemory acl grant … --rotate`. After revoke, deny may take up to **~60s**.
- **`FORBIDDEN`:** no grant for that project — ask the owner; do not spam other projects.
- **`CONFLICT` on `put`:** `get` the record, retry **once** with the correct `if_version`; if still conflicting, escalate (another writer).
- **`NOT_FOUND` on `append`:** create the key with **`put`** first.
- Never print bearer tokens, OAuth values, or raw ACL hashes in chat, logs, or issues.

---

## Human quickstart

```sh
cp .env.example .env          # fill BOTMEMORY_*; never commit .env
go build -o botmemory ./cmd/botmemory
botmemory bootstrap           # once: prints BOTMEMORY_DRIVE_ROOT_ID=
export BOTMEMORY_DRIVE_ROOT_ID=…
botmemory serve               # loopback MCP at http://127.0.0.1:8787/mcp
```

For local dev without Drive: `BOTMEMORY_BACKEND=memory` (data lost on exit).

**ACL (Drive `_acl.json`):** only SHA-256 token hashes are stored. Mint a hash with `botmemory mint-token BOT_ID`, grant with:

```sh
botmemory acl grant <bot> <project> <role> --token-out FILE   # read|write|owner
botmemory acl revoke <bot>
botmemory acl remove <bot>
botmemory acl list                                            # never prints hashes
```

Write tokens only to **`FILE`** (created mode `0600`; must not exist). The CLI does not print raw tokens.

**Tests** (match CI):

```sh
gofmt -l .    # must be empty
go vet ./...
go test -race ./...
# shared box: for p in $(go list ./...); do go test -count=1 -race "$p"; done
```

**Smoke (real Drive):** grant a throwaway `smoke-*` project, then `scripts/smoke_drive.py` (reads env only; never prints the token). See script header and [CONTRIBUTING.md](CONTRIBUTING.md).

**Box setup:** `scripts/setup_box.sh [--test]` builds `~/.local/bin/botmemory` and checks env by name. Override **`BOTMEMORY_BIN`** only with an **absolute path**. Restart: `scripts/restart_serve.sh`.

Optional pilot notes: [docs/pilot-verifychain.md](docs/pilot-verifychain.md).

### Environment (`BOTMEMORY_*`)

| Variable | Purpose |
|----------|---------|
| `BOTMEMORY_ADDR` | Listen address (default `127.0.0.1:8787`; loopback only) |
| `BOTMEMORY_BACKEND` | `drive` (default) or `memory` |
| `BOTMEMORY_DRIVE_ROOT_ID` | Drive vault root folder id |
| `BOTMEMORY_OAUTH_CLIENT_ID` / `SECRET` / `REFRESH_TOKEN` | Drive backend (server-side secrets) |
| `BOTMEMORY_ACL_FILE` | Optional local ACL instead of Drive |
| `BOTMEMORY_LOCK_FILE` | Optional single-instance lock path |

Legacy **`BOTVAULT_*`** names are still read (deprecation warning).

### Safety (summary)

- Loopback bind only; **`Authorization: Bearer`** required; bad/missing token → **401** with `WWW-Authenticate: Bearer`.
- **Single instance** per Drive root per host (lock under `/var/tmp` by default).
- ACL reload ~**60s**; fails closed after repeated load failures (see [SECURITY.md](SECURITY.md)).
- Attribution (`created_by` / `updated_by`) comes from the token; unknown JSON fields on tools are rejected.
- Drive has no true CAS across hosts — do not run two servers against one root on different machines.

## Limits

- **Loopback only** — not exposed to the public internet by design.
- **One server process** per Drive root per host.
- **Not a secrets store** — vault notes are Drive-readable.
- **Deny-by-default ACL** — no wildcard projects.
- **Soft delete only** — `delete` / `reindex` require **owner** role.
- **No cross-host concurrency guarantee** on the same Drive root.

## License

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).

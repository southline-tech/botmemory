# botMemory

Shared memory and handoff notes for AI agent fleets — MCP (HTTP or stdio), **local disk by default**, optional Google Drive. **Not a secrets store.** (Formerly an internal codename; product name is botMemory.)

Also see [AGENTS.md](AGENTS.md) (pasteable bot rules) · [CONTRIBUTING.md](CONTRIBUTING.md) · [SECURITY.md](SECURITY.md)

## For agents

### What / when

Use botMemory for **shared, attributable project state** across bots: decisions, specs, handoffs, assignments, and general notes. Do **not** store secrets. Local vault files are host-readable; Drive vault notes are folder-readable. Threat model: [SECURITY.md](SECURITY.md).

**Habits:** `list` or `search` before `get`; `append` for logs; one structured `put` at end of turn; never paste tokens.

### MCP connection (HTTP)

Streamable HTTP, **`POST`** only, path **`/mcp`**. Default: `http://127.0.0.1:8787/mcp`. Every request needs **`Authorization: Bearer <bot token>`**.

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

Protocol: JSON-RPC 2.0 — `initialize`, `ping`, `tools/list`, `tools/call` (no batch). Compact JSON results; `list` returns **summaries only** — call `list` before `get`.

### MCP connection (stdio)

One child process **per bot**, each with its own token env (shared tokens break ACL attribution):

```json
{
  "mcpServers": {
    "botmemory": {
      "command": "/path/to/botmemory",
      "args": ["stdio"],
      "env": {
        "BOTMEMORY_TOKEN_FILE": "/secure/bot.token",
        "BOTMEMORY_PROJECT": "demo",
        "BOTMEMORY_BACKEND": "local"
      }
    }
  }
}
```

### Bot CLI

First-class commands (alias `bm=botmemory`): `projects`, `list`, `get`, `put`, `append`, `search`, `history`, `stats`, `delete`. Token from `--token-file`, `BOTMEMORY_TOKEN_FILE`, or `BOTMEMORY_TOKEN`. Project from `--project`, `BOTMEMORY_PROJECT`, or ACL `default_project`. Server URL: `BOTMEMORY_URL` (default `http://127.0.0.1:8787/mcp`). `put --file` / `append --file` read body/text from disk (never logged).

```sh
botmemory list --project demo
botmemory put plan --title "Plan" --body "…" --kind spec
botmemory append log --text "- update"
botmemory stats
```

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
| `stats` | `{}` |
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
- **`NOT_FOUND` on `get`:** use `put` or `append` (`append` **creates** missing keys as `kind=note`, v1).
- Never print bearer tokens, OAuth values, or raw ACL hashes in chat, logs, or issues.

---

## Human quickstart (local)

```sh
cp .env.example .env
go build -o botmemory ./cmd/botmemory
botmemory serve                    # local vault under ~/.local/share/botmemory
# grant bots, then connect MCP or: botmemory stdio
```

v0.2 CLI/stdio/local features require **build from source** until the next release tag.

**Optional Drive:** [docs/google-drive-setup.md](docs/google-drive-setup.md). Multi-agent walkthrough: [docs/examples-multi-agent.md](docs/examples-multi-agent.md).

For a **throwaway** Drive smoke vault: `botmemory bootstrap --test-folder` (see script header).

For dev-only ephemeral storage: `BOTMEMORY_BACKEND=memory` (data lost on exit).

**ACL:** only SHA-256 token hashes in `_acl.json`. Mint with `botmemory mint-token BOT_ID`, grant with:

```sh
botmemory acl grant <bot> <project> <role> --token-out FILE [--default-project NAME]
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
| `BOTMEMORY_BACKEND` | `local` (default), `drive`, or `memory` |
| `BOTMEMORY_DATA_DIR` | Local vault root (default XDG / `%LOCALAPPDATA%\\botmemory`) |
| `BOTMEMORY_TOKEN_FILE` / `BOTMEMORY_PROJECT` | Bot CLI / stdio defaults |
| `BOTMEMORY_URL` | MCP URL for CLI (default loopback `/mcp`) |
| `BOTMEMORY_DRIVE_ROOT_ID` | Drive vault root (optional) |
| `BOTMEMORY_OAUTH_CLIENT_ID` / `SECRET` / `REFRESH_TOKEN` | Drive backend (server-side secrets) |
| `BOTMEMORY_ACL_FILE` | Optional local ACL instead of Drive |
| `BOTMEMORY_LOCK_FILE` | Optional single-instance lock path |
| `BOTMEMORY_TEST_FOLDER` | Truthy (`1`/`true`/`yes`) for `bootstrap --test-folder` |

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

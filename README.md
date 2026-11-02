# BotOptimizer

**BotOptimizer Memory** — shared handoff notes for AI agent fleets (MCP HTTP or stdio), **local disk by default**, optional Google Drive. **Not a secrets store.**

Also see [AGENTS.md](AGENTS.md) (pasteable bot rules) · [CONTRIBUTING.md](CONTRIBUTING.md) · [SECURITY.md](SECURITY.md)

## For agents

### What / when

Use BotOptimizer Memory for **shared, attributable project state** across bots: decisions, specs, handoffs, assignments, and general notes. Do **not** store secrets. Local vault files are host-readable; Drive vault notes are folder-readable. Threat model: [SECURITY.md](SECURITY.md).

**Habits:** `list` or `search` before `get`; `append` for logs; one structured `put` at end of turn; never paste tokens.

### MCP connection (HTTP)

Streamable HTTP, **`POST`** only, path **`/mcp`**. Default: `http://127.0.0.1:8787/mcp`. Every request needs **`Authorization: Bearer <bot token>`**.

```json
{
  "mcpServers": {
    "botoptimizer": {
      "url": "http://127.0.0.1:8787/mcp",
      "headers": { "Authorization": "Bearer ${BOTOPTIMIZER_TOKEN}" }
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
    "botoptimizer": {
      "command": "/path/to/botoptimizer",
      "args": ["stdio"],
      "env": {
        "BOTOPTIMIZER_TOKEN_FILE": "/secure/bot.token",
        "BOTOPTIMIZER_PROJECT": "demo",
        "BOTOPTIMIZER_BACKEND": "local"
      }
    }
  }
}
```

### Bot CLI

First-class commands (alias `bm=botoptimizer`): `projects`, `list`, `brief`, `changed`, `get`, `put`, `append`, `search`, `history`, `stats`, `delete`, `version`, `ping`, `usage`, `watch`, `import-usage`. Token from `--token-file`, `BOTOPTIMIZER_TOKEN_FILE`, or `BOTOPTIMIZER_TOKEN`. Project from `--project`, `BOTOPTIMIZER_PROJECT`, or ACL `default_project`. Server URL: `BOTOPTIMIZER_URL` (default `http://127.0.0.1:8787/mcp`). `put --file` / `append --file` read body/text from disk (never logged).

**Usage (v0.4):** Log **real provider token counts** with `usage log --type tokens --in N --out N` whenever the API returns usage. Use proxy etypes only when counts are hidden. **Never fabricate token numbers.** Charts prefer summed tokens; share/CSV are labeled fallbacks. See [examples/usageproxy/README.md](examples/usageproxy/README.md).

```sh
botoptimizer list --project demo
botoptimizer put plan --title "Plan" --body "…" --kind spec
botoptimizer append log --text "- update"
botoptimizer stats
```

### Tools — example `tools/call` arguments

Pass the object below as the tool arguments (exact field names):

| Tool | Example arguments |
|------|-------------------|
| `brief` | `{"project":"demo","limit":40,"if_etag":"…"}` |
| `list` | `{"project":"demo","if_etag":"…"}` |
| `changed` | `{"project":"demo","since":"2026-11-01T12:00:00Z"}` |
| `get` | `{"project":"demo","key":"plan","head":50}` |
| `put` (create) | `{"project":"demo","key":"plan","kind":"spec","title":"Plan","body":"..."}` |
| `put` (update) | `{"project":"demo","key":"plan","title":"Plan","if_version":1}` |
| `append` | `{"project":"demo","key":"plan","text":"\n- update"}` |
| `search` | `{"project":"*","query":"handoff rollout"}` |
| `history` | `{"project":"demo","key":"plan"}` |
| `stats` | `{}` |
| `usage_log` | `{"project":"demo","bot":"my-agent","type":"tokens","in":1200,"out":400,"model":"…"}` |
| `usage_import` | `{"project":"demo","date":"2026-11-01","body":"{…share json…}"}` |
| `usage_import_csv` | `{"project":"demo","date":"2026-11-01","csv":"…","tz":"America/Chicago"}` |
| `watch_check` | `{"project":"demo","since":"2026-11-01"}` |
| `usage_chart` | `{"project":"demo","range":"7d","out":"/tmp/u.html"}` |
| `usage_hints` | `{"project":"demo","since":"2026-11-01T00:00:00Z"}` |
| `reindex` | `{"project":"demo"}` (owner) |
| `delete` | `{"project":"demo","key":"plan"}` (owner; soft archive) |

**`delete` is owner-only** — writers get `FORBIDDEN`; ask the project owner. **`get` still returns archived records** (soft delete); `list` / `search` omit `status=archived` unless you pass `status:"archived"`. Recover via `get` or `history`.

**Kinds (closed set):** `decision`, `spec`, `handoff`, `assignment`, `note`.

**Projects / keys:** regex `^[a-z0-9][a-z0-9._-]{0,63}$` — unique key per project; **no `/` in keys** (use dotted names like `handoff.review`).

### Conventions

- Prefer **`handoff`** keys like `handoff.review` or `handoff.pm` for turn/context handoffs; include next owner, status, and blockers in the body.
- Start sessions with **`brief`** (or `list`); use **`changed`** + `if_etag` to skip unchanged polls.
- Use **`append`** for chronological updates without a version check; use **`put`** + **`if_version`** when replacing structured state.
- Use **`search`** with `"project":"*"` only when you need every granted project; otherwise name the project.
- Archived records are hidden from `list`/`search` unless you filter with `status:"archived"`; **`get` and `history` still read archived keys** (soft delete is recoverable).

### Failure modes

- **Server down / connection refused / timeout:** do not retry in a tight loop — report to the human operator once and wait for restart.
- **`401` / token rejected:** stop; do not mint or rotate tokens. Owner checks ACL / runs `botoptimizer acl grant … --rotate`. After revoke, deny may take up to **~60s**.
- **`FORBIDDEN`:** no grant for that project — ask the owner; do not spam other projects.
- **`CONFLICT` on `put`:** `get` the record, retry **once** with the correct `if_version`; if still conflicting, escalate (another writer).
- **`NOT_FOUND` on `get`:** use `put` or `append` (`append` **creates** missing keys as `kind=note`, v1).
- Never print bearer tokens, OAuth values, or raw ACL hashes in chat, logs, or issues.

---

## Human quickstart (local)

```sh
cp .env.example .env
go build -o botoptimizer ./cmd/botoptimizer
botoptimizer serve                    # local vault under ~/.local/share/botoptimizer
# grant bots, then connect MCP or: botoptimizer stdio
```

v0.2 CLI/stdio/local features require **build from source** until the next release tag.

**Optional Drive:** [docs/google-drive-setup.md](docs/google-drive-setup.md). Multi-agent walkthrough: [docs/examples-multi-agent.md](docs/examples-multi-agent.md).

For a **throwaway** Drive smoke vault: `botoptimizer bootstrap --test-folder` (see script header).

For dev-only ephemeral storage: `BOTOPTIMIZER_BACKEND=memory` (data lost on exit).

**ACL:** only SHA-256 token hashes in `_acl.json`. Mint with `botoptimizer mint-token BOT_ID`, grant with:

```sh
botoptimizer acl grant <bot> <project> <role> --token-out FILE [--default-project NAME]
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

**Box setup:** `scripts/setup_box.sh [--test]` builds `~/.local/bin/botoptimizer` and checks env by name. Override **`BOTOPTIMIZER_BIN`** only with an **absolute path**. Restart: `scripts/restart_serve.sh`.

Optional pilot notes: [docs/pilot-verifychain.md](docs/pilot-verifychain.md).

### Environment (`BOTOPTIMIZER_*`)

| Variable | Purpose |
|----------|---------|
| `BOTOPTIMIZER_ADDR` | Listen address (default `127.0.0.1:8787`; loopback only) |
| `BOTOPTIMIZER_BACKEND` | `local` (default), `drive`, or `memory` |
| `BOTOPTIMIZER_DATA_DIR` | Local vault root (default XDG / `%LOCALAPPDATA%\\botoptimizer`) |
| `BOTOPTIMIZER_TOKEN_FILE` / `BOTOPTIMIZER_PROJECT` | Bot CLI / stdio defaults |
| `BOTOPTIMIZER_URL` | MCP URL for CLI (default loopback `/mcp`) |
| `BOTOPTIMIZER_DRIVE_ROOT_ID` | Drive vault root (optional) |
| `BOTOPTIMIZER_OAUTH_CLIENT_ID` / `SECRET` / `REFRESH_TOKEN` | Drive backend (server-side secrets) |
| `BOTOPTIMIZER_ACL_FILE` | Optional local ACL instead of Drive |
| `BOTOPTIMIZER_LOCK_FILE` | Optional single-instance lock path |
| `BOTOPTIMIZER_TEST_FOLDER` | Truthy (`1`/`true`/`yes`) for `bootstrap --test-folder` |

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
- **Soft delete only** — `delete` / `reindex` require **owner** role (writers see `FORBIDDEN`).
- **Windows:** lock and token files are created without `O_NOFOLLOW`; avoid symlinked paths in untrusted directories (see [SECURITY.md](SECURITY.md)).
- **No cross-host concurrency guarantee** on the same Drive root.

## License

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).

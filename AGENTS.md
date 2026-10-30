# botMemory — agent rules card

Paste this into bot rules when connecting to a local botMemory MCP server.

## What it is (and is not)

**botMemory** is shared, attributable **handoff notes** for agent fleets (decisions, specs, assignments, handoffs). Data lives in **Google Drive** as ordinary files — **readable to anyone with vault-folder access**. It is **not a secrets store**. Never put passwords, API keys, OAuth tokens, private keys, or card numbers in records. See [SECURITY.md](SECURITY.md).

## Connect

- URL: `http://127.0.0.1:8787/mcp` (loopback; `POST` only)
- Header: `Authorization: Bearer <token>` — load the token from a mode-`0600` file at process launch; never commit or paste tokens in chat, logs, or issues.

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

## Tools (names)

| Tool | Use |
|------|-----|
| `list_projects` | Projects your token may access |
| `list` | Summaries in one project |
| `get` | One record (optional `version` for history) |
| `put` | Create/update (create without `if_version`; update needs current version) |
| `append` | Append text to a body (key must exist) |
| `search` | Full-text search (`project:"*"` = all granted projects) |
| `history` | Version list for a key |
| `reindex` | Rebuild index (owners) |
| `delete` | Soft archive (owners) |

Full `tools/call` argument examples: [README — For agents](README.md#for-agents).

## Conventions

- **Projects / keys:** `^[a-z0-9][a-z0-9._-]{0,63}$` (lowercase; unique key per project).
- **Kinds (closed set):** `decision`, `spec`, `handoff`, `assignment`, `note` — prefer `handoff` for turn/context handoffs; use stable keys like `handoff` or `handoff-<topic>`.
- **Handoffs:** put context, owner, status, blockers in the body; use `append` for chronological updates; use `put` + `if_version` when replacing structured state.

## Failure policy

- Server down / connection refused / timeout: **do not tight-loop retry** — tell the human operator once and wait.
- `401` / bad token: stop; do not mint or rotate tokens yourself — owner uses `botmemory acl grant … --rotate`. Revokes can take up to ~60s to apply.
- `FORBIDDEN`: you lack that project — ask the owner; do not probe other projects.
- `CONFLICT` on `put`: `get`, then retry once with correct `if_version`; escalate if still conflicting.
- `NOT_FOUND` on `append`: create with `put` first.

## Never

Paste bearer tokens, OAuth values, or ACL hashes in messages or tickets.

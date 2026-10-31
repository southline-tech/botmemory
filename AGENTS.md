# botMemory — agent rules card

Paste into bot rules when using botMemory MCP (HTTP or stdio).

## What it is (and is not)

**botMemory** is shared, attributable **handoff notes** for agent fleets. Default storage is **local disk**; optional **Google Drive**. Content is **not encrypted secrets** — treat vault data as readable on the host (and on Drive if used). Never store passwords, API keys, OAuth tokens, or private keys in records. See [SECURITY.md](SECURITY.md).

## Connect (HTTP)

- URL: `http://127.0.0.1:8787/mcp` (`POST` only)
- Header: `Authorization: Bearer <token>` from a mode-`0600` file at launch

## Connect (stdio)

One MCP server process **per bot** with its own `BOTMEMORY_TOKEN` or `BOTMEMORY_TOKEN_FILE`. Optional `BOTMEMORY_PROJECT` or ACL `default_project` so you can omit `project` on tools.

```json
{
  "mcpServers": {
    "botmemory": {
      "command": "/path/to/botmemory",
      "args": ["stdio"],
      "env": {
        "BOTMEMORY_TOKEN_FILE": "/secure/bot.token",
        "BOTMEMORY_BACKEND": "local"
      }
    }
  }
}
```

## Tools

| Tool | Use |
|------|-----|
| `list_projects` | Granted projects (`default` flag when set) |
| `list` | **Call before get** — summaries only |
| `get` | Full record |
| `put` | Create/update; matching `if_version` + unchanged fields = no-op |
| `append` | Append text; **creates** missing key as `note` |
| `search` | Query summaries |
| `history` | Versions for a key |
| `stats` | Process call counters (no note content) |
| `reindex` / `delete` | Owners only (`delete` soft-archives; writers get FORBIDDEN) |

- **`get` / `history`:** include **archived** records; **`list` / `search`** hide archived unless `status:"archived"`.

## CLI (same vault)

```sh
export BOTMEMORY_TOKEN_FILE=/secure/bot.token
export BOTMEMORY_PROJECT=demo
botmemory list
botmemory put plan --title T --body B --kind spec
botmemory append log --text "- item"
```

Compact JSON stdout; errors `CODE: …; hint: …` on stderr.

## Habits

- **Orient** with `list` or `search`, not a hand-maintained index key.
- **Append** chronological logs; **put** + `if_version` for structured replacements.
- **One write** at end of turn when possible.
- **Never** paste tokens or note bodies containing secrets.

## Failures

- Down/timeout: report once; no tight retry loops.
- `FORBIDDEN`: ask owner for `acl grant`.
- `CONFLICT`: `get`, retry `put` once with `if_version`.
- `NOT_FOUND` on `get`: `put` or `append` (append creates).

Example flows: [docs/examples-multi-agent.md](docs/examples-multi-agent.md).

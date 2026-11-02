# BotOptimizer Memory — agent rules card

Paste into bot rules when using BotOptimizer MCP (HTTP or stdio).

## What it is (and is not)

**BotOptimizer Memory** is shared, attributable **handoff notes** for agent fleets. Default storage is **local disk**; optional **Google Drive**. Content is **not encrypted secrets** — treat vault data as readable on the host (and on Drive if used). Never store passwords, API keys, OAuth tokens, or private keys in records. See [SECURITY.md](SECURITY.md).

## Connect (HTTP)

- URL: `http://127.0.0.1:8787/mcp` (`POST` only)
- Header: `Authorization: Bearer <token>` from a mode-`0600` file at launch

## Connect (stdio)

One MCP server process **per bot** with its own `BOTOPTIMIZER_TOKEN` or `BOTOPTIMIZER_TOKEN_FILE`. Optional `BOTOPTIMIZER_PROJECT` or ACL `default_project` so you can omit `project` on tools.

```json
{
  "mcpServers": {
    "botoptimizer": {
      "command": "/path/to/botoptimizer",
      "args": ["stdio"],
      "env": {
        "BOTOPTIMIZER_TOKEN_FILE": "/secure/bot.token",
        "BOTOPTIMIZER_BACKEND": "local"
      }
    }
  }
}
```

## Tools

| Tool | Use |
|------|-----|
| `list_projects` | Granted projects (`default` flag when set) |
| `brief` | Orient in ~2k tokens; use `if_etag` to skip unchanged |
| `list` | **Call before get** — summaries only; optional `if_etag` |
| `changed` | Keys updated after `since` (RFC3339) |
| `get` | Full or partial body (`head`, `tail`, `since`, `max_bytes`, `summary`) |
| `put` | Create/update; matching `if_version` + unchanged fields = no-op |
| `append` | Append text; **creates** missing key; `--kind` handoff|spec|decision|note|usage (default note) |
| `search` | Query summaries |
| `history` | Versions for a key |
| `stats` | Process call counters (no note content) |
| `reindex` / `delete` | Owners only (`delete` soft-archives; writers get FORBIDDEN) |
| `usage_log` | Append one line to `log.usage.<bot>` — **real `tokens` in/out from provider API first** |
| `usage_import` | Hosted fleet share JSON → `usage.share.<date>` (fallback charts) |
| `usage_import_csv` | Dashboard CSV ground truth → `usage.csv.<date>` |
| `watch_check` / `watch_init` | Token budget rules **before** proxy rules |
| `usage_chart` | Offline HTML+SVG (`--range` 24h–90d); `explain` at timestamp |
| `usage_hints` | Waste patterns + `token_spike` |

**Usage:** Log **`tokens`** with provider-reported `in`/`out`/`cache`/`model` whenever available. Use proxy etypes (`wake`, `bytes_r`, `tool`, …) only when counts are hidden. **Never fabricate token numbers.**

- **`get` / `history`:** include **archived** records; **`list` / `search`** hide archived unless `status:"archived"`.

## CLI (same vault)

```sh
export BOTOPTIMIZER_TOKEN_FILE=/secure/bot.token
export BOTOPTIMIZER_PROJECT=demo
botoptimizer list
botoptimizer put plan --title T --body B --kind spec
botoptimizer append log --text "- item"
```

Compact JSON stdout; errors `CODE: …; hint: …` on stderr.

## Habits

- **Orient** with **`brief`**, `list`, or `search` — not a hand-maintained index key.
- **Append** chronological logs; **put** + `if_version` for structured replacements.
- **One write** at end of turn when possible.
- **Never** paste tokens or note bodies containing secrets.

## Failures

- Down/timeout: report once; no tight retry loops.
- `FORBIDDEN`: ask owner for `acl grant`.
- `CONFLICT`: `get`, retry `put` once with `if_version`.
- `NOT_FOUND` on `get`: `put` or `append` (append creates).

Example flows: [docs/examples-multi-agent.md](docs/examples-multi-agent.md).

Keys/projects: `^[a-z0-9][a-z0-9._-]{0,63}$` — **slashes are invalid**; use dots (`handoff.review`, `log.apps`).

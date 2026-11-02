# Multi-agent example (implementor → reviewer → PM)

Three bots share one project (`shipit`) with different roles. Keys follow `^[a-z0-9][a-z0-9._-]{0,63}$`.

| Bot | Role | Typical keys |
|-----|------|----------------|
| implementor | write | `spec.feature`, `log.impl` |
| reviewer | write | `log.review`, `handoff.review` |
| pm | owner | `plan`, `handoff.pm` |

Keys use dots only — **no `/`** in key names (`feedback.slug.date`, not `feedback/slug`).

**Orientation:** call `list` or `search` before `get` — list returns summaries only (title + body prefix), not full bodies.

## MCP `tools/call` arguments

| Tool | implementor | reviewer | PM |
|------|-------------|----------|-----|
| `list_projects` | `{}` | `{}` | `{}` |
| `brief` | `{"project":"shipit"}` | same | same |
| `changed` | `{"project":"shipit","since":"2026-11-01T00:00:00Z"}` | same | same |
| `list` | `{"project":"shipit"}` | same | same |
| `get` | `{"project":"shipit","key":"spec.feature"}` | … | … |
| `put` | `{"project":"shipit","key":"spec.feature","kind":"spec","title":"…","body":"…","if_version":N}` | … | … |
| `append` | `{"project":"shipit","key":"log.impl","text":"\n- done X"}` | creates key if missing (`kind=note`) | … |
| `search` | `{"project":"shipit","query":"blocker"}` | … | `{"project":"*","query":"handoff"}` |
| `history` | `{"project":"shipit","key":"spec.feature"}` | … | … |
| `stats` | `{}` | `{}` | `{}` |
| `delete` | forbidden | forbidden | `{"project":"shipit","key":"…"}` (owner) |

Omit `project` when the bot token has ACL `default_project` set (`botoptimizer acl grant … --default-project shipit`).

## CLI one-liners

Assume `BOTOPTIMIZER_TOKEN_FILE` and `BOTOPTIMIZER_PROJECT=shipit` (or `--project shipit`).

```sh
botoptimizer brief
botoptimizer changed --since 2026-11-01T00:00:00Z
botoptimizer get spec.feature --head 40
botoptimizer put spec.feature --title "Feature" --body "…" --kind spec
botoptimizer append log.impl --text "- shipped slice 1"
botoptimizer search "blocker" --limit 10
botoptimizer stats
```

Compact JSON on stdout; `--human` or `BOTOPTIMIZER_HUMAN=1` for indented output. Errors on stderr: `CODE: message; hint: …` with exit codes 3=NOT_FOUND, 4=FORBIDDEN, 5=CONFLICT, 6=INVALID.

## Stdio MCP (one process per bot)

```json
{
  "mcpServers": {
    "botoptimizer-implementor": {
      "command": "/path/to/botoptimizer",
      "args": ["stdio"],
      "env": {
        "BOTOPTIMIZER_TOKEN_FILE": "/secure/implementor.token",
        "BOTOPTIMIZER_PROJECT": "shipit",
        "BOTOPTIMIZER_BACKEND": "local"
      }
    }
  }
}
```

Do not share one token across multiple bot identities in a shared MCP host — attribution comes from the token. No `as=` impersonation.

## Handoff convention

Use keys like `handoff.review` or `handoff.pm` with kind `handoff`. Append chronological updates; use `put` + `if_version` when replacing structured state.

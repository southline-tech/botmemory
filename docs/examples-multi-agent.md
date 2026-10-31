# Multi-agent example (implementor → reviewer → PM)

Three bots share one project (`shipit`) with different roles. Keys follow `^[a-z0-9][a-z0-9._-]{0,63}$`.

| Bot | Role | Typical keys |
|-----|------|----------------|
| implementor | write | `spec.feature`, `log.impl` |
| reviewer | write | `log.review`, `handoff.review` |
| pm | owner | `plan`, `handoff.pm` |

**Orientation:** call `list` or `search` before `get` — list returns summaries only (title + body prefix), not full bodies.

## MCP `tools/call` arguments

| Tool | implementor | reviewer | PM |
|------|-------------|----------|-----|
| `list_projects` | `{}` | `{}` | `{}` |
| `list` | `{"project":"shipit"}` | same | same |
| `get` | `{"project":"shipit","key":"spec.feature"}` | … | … |
| `put` | `{"project":"shipit","key":"spec.feature","kind":"spec","title":"…","body":"…","if_version":N}` | … | … |
| `append` | `{"project":"shipit","key":"log.impl","text":"\n- done X"}` | creates key if missing (`kind=note`) | … |
| `search` | `{"project":"shipit","query":"blocker"}` | … | `{"project":"*","query":"handoff"}` |
| `history` | `{"project":"shipit","key":"spec.feature"}` | … | … |
| `stats` | `{}` | `{}` | `{}` |
| `delete` | forbidden | forbidden | `{"project":"shipit","key":"…"}` (owner) |

Omit `project` when the bot token has ACL `default_project` set (`botmemory acl grant … --default-project shipit`).

## CLI one-liners

Assume `BOTMEMORY_TOKEN_FILE` and `BOTMEMORY_PROJECT=shipit` (or `--project shipit`).

```sh
botmemory list
botmemory get spec.feature
botmemory put spec.feature --title "Feature" --body "…" --kind spec
botmemory append log.impl --text "- shipped slice 1"
botmemory search "blocker" --limit 10
botmemory stats
```

Compact JSON on stdout; `--human` or `BOTMEMORY_HUMAN=1` for indented output. Errors on stderr: `CODE: message; hint: …` with exit codes 3=NOT_FOUND, 4=FORBIDDEN, 5=CONFLICT, 6=INVALID.

## Stdio MCP (one process per bot)

```json
{
  "mcpServers": {
    "botmemory-implementor": {
      "command": "/path/to/botmemory",
      "args": ["stdio"],
      "env": {
        "BOTMEMORY_TOKEN_FILE": "/secure/implementor.token",
        "BOTMEMORY_PROJECT": "shipit",
        "BOTMEMORY_BACKEND": "local"
      }
    }
  }
}
```

Do not share one token across multiple bot identities in a shared MCP host — attribution comes from the token. No `as=` impersonation.

## Handoff convention

Use keys like `handoff.review` or `handoff.pm` with kind `handoff`. Append chronological updates; use `put` + `if_version` when replacing structured state.

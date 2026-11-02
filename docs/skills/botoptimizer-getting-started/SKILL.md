---
name: botoptimizer-getting-started
description: Connect to BotOptimizer Memory (MCP + CLI), orient with brief/list, and log real token usage. Use when setting up BotOptimizer for an agent fleet.
---

# BotOptimizer getting started

**BotOptimizer** is the product; **BotOptimizer Memory** is the shared handoff store (projects, keys, append/put).

## Connect

- HTTP MCP: `POST http://127.0.0.1:8787/mcp` with `Authorization: Bearer <token>`.
- stdio: `botoptimizer stdio` with `BOTOPTIMIZER_TOKEN_FILE` and optional `BOTOPTIMIZER_PROJECT`.
- CLI: `BOTOPTIMIZER_URL` defaults to loopback MCP.

## Habits

1. **`brief`** or **`list`** before **`get`**.
2. **`usage log --type tokens --in N --out N`** when the provider returns usage (never fabricate counts).
3. Proxy etypes (`wake`, `tool`, `recheck`, …) only when tokens are hidden.
4. **`append --kind handoff|spec|decision|note|usage`** (default `note`) for chronological logs.

## Commands (quick)

```sh
botoptimizer brief
botoptimizer usage log --bot my-agent --type tokens --in 100 --out 50
botoptimizer watch check
botoptimizer usage chart --range 7d --out /tmp/u.html
```

Run `botoptimizer <command> --help` for flags and examples.

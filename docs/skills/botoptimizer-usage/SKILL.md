---
name: botoptimizer-usage
description: Log and analyze BotOptimizer usage — tokens, imports, CSV, charts, watches, hints. Use for optimizer telemetry and cost/waste workflows.
---

# BotOptimizer usage

## Log (primary: real tokens)

```sh
botoptimizer usage log --bot SLUG --type tokens --in 1200 --out 400 --model MODEL [--cache N] [--cost USD] [--ref ID]
```

Omitting `--in`/`--out` for `tokens` is **INVALID**. Use proxy types only when the host hides counts:

`wake|bytes_r|bytes_w|tool|subagent|recheck|msg|image|retry|read|other` with `--n`, `--bytes`, `--tool`, `--key`, `--ref`.

MCP: `usage_log` with the same fields.

## Fleet share import

```sh
botoptimizer usage import --date YYYY-MM-DD --file shares.json [--merge]
```

Writes `usage.share.<date>` (fallback charts; does not replace real token series).

## CSV ground truth

```sh
botoptimizer import-usage export.csv --date YYYY-MM-DD [--tz America/Chicago] [--map time=timestamp,bot=agent,...]
```

Zoneless timestamps require `--tz` (`CSV_TZ_REQUIRED` otherwise).

## Watches and hints

```sh
botoptimizer watch init
botoptimizer watch check [--bot SLUG] [--since RFC3339|YYYY-MM-DD]
botoptimizer usage hints [--bot SLUG] [--since T]
```

Token rules in `watch.rules` run **before** proxy rules.

## Chart

```sh
botoptimizer usage chart --range 7d --out /tmp/usage.html
botoptimizer usage chart --explain --at 2026-11-01T12:00:00Z
```

Range **24h–90d** only (`RANGE_INVALID` outside). Prefers summed real tokens; share/CSV modes are labeled in HTML.

See also: [examples/usageproxy/README.md](../../examples/usageproxy/README.md).

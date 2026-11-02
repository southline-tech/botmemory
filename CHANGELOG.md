# Changelog

## Unreleased (v0.4 + v0.5)

### BotOptimizer rename (v0.5)

- Product CLI and binary: **BotOptimizer** (`botoptimizer`); Go module `github.com/southline-tech/botoptimizer`.
- Environment prefix **BOTOPTIMIZER_***, default data dir `~/.local/share/botoptimizer`.
- MCP server name `botoptimizer`. **BotOptimizer Memory** is the shared handoff store (formerly “botMemory”).

### CLI (v0.5 — RAD-305)

- Per-command **`--help`** on all subcommands (usage line, flags, example).
- **`append --kind`**: `handoff|spec|decision|note|usage` (default `note`); invalid kind → `KIND_INVALID`.

### Optimizer usage (v0.4)

- **`usage log` / `usage_log`**: real provider **`tokens`** (`in`/`out`/optional `cache`/`model`/`cost`) on `log.usage.<slug>`; proxy etypes when counts are hidden; never fabricate tokens.
- **`usage import`**: hosted fleet share JSON → `usage.share.<date>`.
- **`import-usage` / `usage_import_csv`**: dashboard CSV import with header detect, `--tz`, idempotent dedupe.
- **`watch check` / `watch_init`**: rules in `watch.rules`; token budgets evaluated before proxy rules.
- **`usage chart`**: offline HTML+SVG; `--range` 24h–90d; `explain --at`.
- **`usage hints`**: waste patterns including `token_spike`.
- Partial **`get`** metadata (`mode`, `truncated`, `n`); **`brief`/`changed`/`list`** etag and date forms (v0.3 carryover in this release note).

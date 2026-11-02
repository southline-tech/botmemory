# Security policy

BotOptimizer is an MCP server for **BotOptimizer Memory** — shared handoff notes — with **local disk** storage by default and optional **Google Drive**. Notes are ordinary files — **readable on the host** (and on Drive when used). It is **explicitly not a secrets vault**. Do not store passwords, API keys, OAuth tokens, private keys, payment card numbers, or similar material in BotOptimizer Memory records.

## Supported versions

| Version | Supported |
| ------- | --------- |
| 0.1.x   | Yes       |
| Before 0.1 | No        |

Security fixes land on the **latest 0.1.x** release first, then are backported to earlier 0.1.x lines as needed.

## Reporting a vulnerability

**Do not open a public GitHub issue for security bugs.**

Report vulnerabilities **privately** using [GitHub Security Advisories](https://github.com/southline-tech/botmemory/security/advisories/new):

https://github.com/southline-tech/botmemory/security/advisories/new

Include:

- Affected version or commit SHA
- Steps to reproduce (no live secrets)
- Impact assessment
- Optional patch suggestion

**Never** paste production bearer tokens, OAuth refresh tokens, client secrets, or raw `_acl.json` token hashes into the report. Redact values and describe them instead.

## Response expectations

- **Initial acknowledgment:** within **5 business days** (America/Chicago).
- **Status update:** within **14 calendar days** of acknowledgment if the issue remains open.
- Fix and advisory timing depend on severity; critical loopback-authentication bypasses are prioritized.

## Threat model

### What BotOptimizer protects

- Bot access is gated by **bearer tokens** sent as `Authorization: Bearer <token>` to the MCP endpoint (default `http://127.0.0.1:8787/mcp`).
- Only **SHA-256 hashes** of tokens are stored in Drive `_acl.json`; raw tokens never belong in the vault.
- The server binds to **loopback only** (`BOTOPTIMIZER_ADDR` must be a loopback address). Non-loopback binds are refused.
- **Deny by default:** tokens see only projects explicitly granted in `_acl.json`; there is no wildcard project access.

### What BotOptimizer does not protect

1. **Drive-readable notes** — vault content is visible to anyone with Drive access to the root folder. Use a dedicated secrets manager for credentials, not BotOptimizer Memory records.
2. **Host compromise** — anyone who can run code as the BotOptimizer process (or read its memory/env) can access the vault for that Drive root.
3. **Multi-host concurrency** — the single-instance lock is **per host** (Unix: `flock`; Windows: `LockFileEx`). Running two servers against one vault root on different hosts is unsupported.
4. **Instant revocation** — `_acl.json` reloads about every **60 seconds**.

### Operational guidance

- Treat raw bot tokens like passwords. Rotate with `botoptimizer acl grant … --rotate`; revoke via ACL (`revoke` / `remove`).
- Keep server-side **`BOTOPTIMIZER_OAUTH_*`** credentials on the host only; never commit them; never put them in Drive notes.
- **Unix:** lock files and ACL token files use `O_NOFOLLOW` on the last path component. **Windows:** those opens do not use `O_NOFOLLOW` — create lock and token paths in directories you control (no untrusted symlinks on the path).

## Preferred language

Product name: **BotOptimizer**. Handoff store: **BotOptimizer Memory**. Environment variables: **`BOTOPTIMIZER_*`**.

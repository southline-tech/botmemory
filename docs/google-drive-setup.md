# Google Drive setup (optional)

botMemory defaults to **local disk** storage. Use Drive when you want a shared vault in Google Drive (still not a secrets store).

## Overview

1. Create OAuth credentials (Desktop app or similar) with scope `https://www.googleapis.com/auth/drive.file`.
2. Obtain a refresh token for the server host (one-time OAuth flow; keep refresh token on the server only).
3. Run `botmemory bootstrap` to create or adopt the vault root folder; set `BOTMEMORY_DRIVE_ROOT_ID`.
4. Set `BOTMEMORY_BACKEND=drive` (recommended; legacy: root id alone selects drive with a deprecation warning).

## Environment

| Variable | Purpose |
|----------|---------|
| `BOTMEMORY_BACKEND` | `drive` |
| `BOTMEMORY_DRIVE_ROOT_ID` | Root folder id from bootstrap |
| `BOTMEMORY_OAUTH_CLIENT_ID` | OAuth client id |
| `BOTMEMORY_OAUTH_CLIENT_SECRET` | OAuth client secret |
| `BOTMEMORY_OAUTH_REFRESH_TOKEN` | Refresh token (server only) |

Alternatively use Application Default Credentials with the same Drive scope on hosts that support ADC.

## Bootstrap

```sh
export BOTMEMORY_OAUTH_CLIENT_ID=…
export BOTMEMORY_OAUTH_CLIENT_SECRET=…
export BOTMEMORY_OAUTH_REFRESH_TOKEN=…
botmemory bootstrap
# prints BOTMEMORY_DRIVE_ROOT_ID=…
botmemory serve --storage gdrive
```

Throwaway vault for tests: `botmemory bootstrap --test-folder` (never production).

## Migrate local → Drive

After Drive is configured:

```sh
export BOTMEMORY_DATA_DIR=~/.local/share/botmemory   # if non-default
export BOTMEMORY_DRIVE_ROOT_ID=…
botmemory migrate --from local --to drive --dry-run
botmemory migrate --from local --to drive            # add --force if dest has data
```

Drive → local is not supported. Source files are kept unless you pass `--delete-source`.

## ACL on Drive

Same as local: `_acl.json` at the vault root stores **hashes only**. Use `botmemory acl grant … --token-out FILE`. Reload ~60s.

See [SECURITY.md](../SECURITY.md) for threat model (Drive-readable notes, single-host lock, multi-host unsupported).

# Google Drive setup (optional)

botMemory defaults to **local disk** storage. Use Drive when you want a shared vault in Google Drive (still not a secrets store).

## Overview

1. Create OAuth credentials (Desktop app or similar) with scope `https://www.googleapis.com/auth/drive.file`.
2. Obtain a refresh token for the server host (one-time OAuth flow; keep refresh token on the server only).
3. Run `botoptimizer bootstrap` to create or adopt the vault root folder; set `BOTOPTIMIZER_DRIVE_ROOT_ID`.
4. Set `BOTOPTIMIZER_BACKEND=drive` (recommended; legacy: root id alone selects drive with a deprecation warning).

## Environment

| Variable | Purpose |
|----------|---------|
| `BOTOPTIMIZER_BACKEND` | `drive` |
| `BOTOPTIMIZER_DRIVE_ROOT_ID` | Root folder id from bootstrap |
| `BOTOPTIMIZER_OAUTH_CLIENT_ID` | OAuth client id |
| `BOTOPTIMIZER_OAUTH_CLIENT_SECRET` | OAuth client secret |
| `BOTOPTIMIZER_OAUTH_REFRESH_TOKEN` | Refresh token (server only) |

Alternatively use Application Default Credentials with the same Drive scope on hosts that support ADC.

## Bootstrap

```sh
export BOTOPTIMIZER_OAUTH_CLIENT_ID=…
export BOTOPTIMIZER_OAUTH_CLIENT_SECRET=…
export BOTOPTIMIZER_OAUTH_REFRESH_TOKEN=…
botoptimizer bootstrap
# prints BOTOPTIMIZER_DRIVE_ROOT_ID=…
botoptimizer serve --storage gdrive
```

Throwaway vault for tests: `botoptimizer bootstrap --test-folder` (never production).

## Migrate local → Drive

After Drive is configured:

```sh
export BOTOPTIMIZER_DATA_DIR=~/.local/share/botoptimizer   # if non-default
export BOTOPTIMIZER_DRIVE_ROOT_ID=…
botoptimizer migrate --from local --to drive --dry-run
botoptimizer migrate --from local --to drive            # add --force if dest has data
```

Drive → local is not supported. Source files are kept unless you pass `--delete-source`.

## ACL on Drive

Same as local: `_acl.json` at the vault root stores **hashes only**. Use `botoptimizer acl grant … --token-out FILE`. Reload ~60s.

See [SECURITY.md](../SECURITY.md) for threat model (Drive-readable notes, single-host lock, multi-host unsupported).

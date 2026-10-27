# verifychain-ai pilot (RAD-280 M3)

Three bots share one botVault project, `verifychain`. No other project is
granted, and there are no wildcards.

| Bot | Role | Can do |
|---|---|---|
| `pm-verifychain` | `owner` | everything, plus `delete` (archive) and `reindex` |
| `implementor-verifychain` | `write` | `get/list/search/history` plus `put/append` |
| `reviewer-verifychain` | `write` | `get/list/search/history` plus `put/append` (records its review findings) |

**Role decision (PM, M3):** the reviewer gets `write` so it can record findings
in botVault (for example appending to a `review-*` record). No append-only role is
added. All writes are attributed via `updated_by` from the token, so reviewer
edits are always distinguishable; only the PM (`owner`) can archive or reindex.

## Granting (needs Brett's approval for real Drive)

On the botVault host, using the same env as `serve` (OAuth env plus `BOTVAULT_DRIVE_ROOT_ID`):

```sh
scripts/pilot_verifychain.sh                                   # dry run: shows the 3 grant commands
BOTVAULT_PILOT_ALLOW_DRIVE=1 scripts/pilot_verifychain.sh --apply
```

Each grant mints a fresh token and writes **only its SHA-256 hash** into
`_acl.json` (conditional on the Drive file version, retried on a concurrent
edit). It writes the raw token to `/workspace/loops/RAD-280-pilot/<bot>/botvault.token`
with mode 0600. That file must not already exist, and a symlink there is refused. `O_NOFOLLOW`
only checks the final path component, so the script creates each per-bot
directory with mode 0700 under `/workspace/loops/RAD-280-pilot/` (override with `PILOT_TOKEN_DIR`). If that dir or a per-bot dir already exists, the script **refuses** (exit 3, never chmods) unless it is a real directory owned by you with mode 0700. If the ACL write fails
after the token file was written, the CLI re-reads the ACL. If the new hash is
there, the grant succeeded. Otherwise the file is **kept** (Drive may still
apply the write late) and the CLI reports the outcome as unknown, giving the
file path. Check `botvault acl list` later: if the bot is listed, the token is
live; if not, delete the file. Grant and revoke report success only after a
re-read shows the change. Only one `botvault acl` run per vault at a time on
this host: a second run is refused while the per-vault ACL lock is held.

## Getting each bot its token (never via chat)

- Never paste a token into chat, Slack, Linear, an email, a commit, or a prompt.
  Don't `cat` it into a terminal that is being recorded or logged.
- **Same host (the pilot setup):** all pilot bots run as the same `box` Unix
  user, so there is no per-bot owner to `install -o` to, and Unix permissions
  can't separate one bot's token from another's. Keep each token where it was
  minted, `/workspace/loops/RAD-280-pilot/<bot>/botvault.token` (file 0600, dir
  0700), and have each bot's launcher read **only its own** file. Don't copy it
  anywhere else. Separation between bots is by convention plus the ACL roles,
  not by OS user.
- **Different host:** copy over an authenticated channel (`scp`/`rsync` over SSH)
  straight into the target path, then delete the staging copy. Don't use a
  pastebin, shared drive, or chat attachment.
- If a token might have been exposed, **rotate** it (see below) rather than
  reasoning about it.

## MCP config for each bot

botVault listens only on loopback: `http://127.0.0.1:8787/mcp` (Streamable HTTP).
The bot sends `Authorization: Bearer <token>`. Load the token from its file at
launch so it never sits in a config that gets committed or shared:

```sh
export BOTVAULT_TOKEN="$(cat /workspace/loops/RAD-280-pilot/<bot>/botvault.token)"   # in the bot's launcher, not a dotfile in git
```

```json
{
  "mcpServers": {
    "botvault": {
      "url": "http://127.0.0.1:8787/mcp",
      "headers": { "Authorization": "Bearer ${BOTVAULT_TOKEN}" }
    }
  }
}
```

(The exact env interpolation syntax depends on the MCP client. If the client
can't interpolate, generate the config at launch into a mode-600 file outside
git.) Every call names `project: "verifychain"`, and other projects are denied.

## Revoke / rotate

```sh
botvault acl list                                    # bots, roles, active/REVOKED; no hashes
botvault acl revoke reviewer-verifychain             # stops working within 60s
botvault acl remove reviewer-verifychain             # delete the entry entirely
botvault acl grant reviewer-verifychain verifychain write --rotate \
  --token-out /workspace/loops/RAD-280-pilot/reviewer-verifychain/botvault.token.new
```

`revoke` applies to the **bot as a whole**: it has one token, so revoking
disables it for every project it holds, not just `verifychain`. It refuses
to revoke the last active owner of any project (for example `pm-verifychain` on
`verifychain`) unless you pass `--force`. Grant another owner first.

`--rotate` replaces the hash (so the old token dies) and clears `revoked`. A
revoked bot can only be re-enabled with `--rotate`. Roles are kept when a bot is
revoked. Note the README caveat: while `_acl.json` is unreadable, the last good
ACL stays in force for up to about 5 minutes.

## Tested

Only against memory and the fake Drive (`internal/acladmin`,
`internal/drive` `TestACLAdminOnDrive`, `cmd/botvault` `TestACLCommandMemory`).
On real Drive, 2026-10-26: the three pilot bots (pm-verifychain owner,
implementor-verifychain write, reviewer-verifychain write, on `verifychain`
only) were granted and smoke-tested. Each token did put, get and search on
verifychain and was denied on other projects. A throwaway bot, `qa-throwaway`,
was granted, revoked, re-granted and revoked again, and then removed with
`botvault acl remove`. `acl list` now shows exactly the three pilot bots, all
active.

## Server environment and restart (names only, never values)

`botvault serve` reads these (from a grep of every `os.Getenv` in the code):

| Variable | Needed for | Notes |
|---|---|---|
| `BOTVAULT_OAUTH_CLIENT_ID` | serve (drive) | box env secret |
| `BOTVAULT_OAUTH_CLIENT_SECRET` | serve (drive) | box env secret |
| `BOTVAULT_OAUTH_REFRESH_TOKEN` | serve (drive) | loaded from `/workspace/loops/RAD-280-oauth/refresh_token` (0600) at launch |
| `BOTVAULT_DRIVE_ROOT_ID` | serve, acl (drive) | loaded from `/workspace/loops/RAD-280-oauth/drive_root_id` |
| `BOTVAULT_ADDR` | serve | optional, default `127.0.0.1:8787`; non-loopback binds are refused |
| `BOTVAULT_BACKEND` | all | optional: `drive` (default) or `memory` (tests only) |
| `BOTVAULT_LOCK_FILE` | serve | optional instance-lock override |
| `BOTVAULT_ACL_FILE` | serve | optional local ACL file instead of Drive `_acl.json`; `acl` refuses to run while it's set |
| `BOTVAULT_ACL_LOCK_FILE` | acl | optional ACL-lock override |

`BOTVAULT_OAUTH_REDIRECT_URL` is also a box env secret. It was used once for the
consent and code exchange; `serve` doesn't read it.

The OAuth client id and secret live in the **box's env secrets** (already in the
environment). They are never in a `.env` file, the repo, or chat.

Restart recipe. The binary isn't on PATH; `scripts/setup_box.sh` builds it at
`$HOME/.local/bin/botvault` (`/home/box/.local/bin/botvault` on the box; override
with `BOTVAULT_BIN`). Check it exists **before** stopping the running server:

```sh
BIN=${BOTVAULT_BIN:-$HOME/.local/bin/botvault}
[ -x "$BIN" ] || scripts/setup_box.sh || exit 1        # build first; never stop the server without a binary
[ -x "$BIN" ] || { echo "no botvault binary at $BIN" >&2; exit 1; }
export BOTVAULT_OAUTH_REFRESH_TOKEN="$(cat /workspace/loops/RAD-280-oauth/refresh_token)"
export BOTVAULT_DRIVE_ROOT_ID="$(cat /workspace/loops/RAD-280-oauth/drive_root_id)"
: "${BOTVAULT_OAUTH_CLIENT_ID:?missing}" "${BOTVAULT_OAUTH_CLIENT_SECRET:?missing}"
pkill -f 'botvault serve' || true                       # stop only now (the flock is released on exit)
nohup "$BIN" serve >>/workspace/loops/RAD-280-pilot/serve.log 2>&1 &   # binds 127.0.0.1:8787
sleep 2
# health: there is no /health endpoint, so an unauthenticated POST to /mcp
# should get 401 (server up, auth enforced)
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://127.0.0.1:8787/mcp   # expect 401
```

The server log (stdout and stderr) is appended to
`/workspace/loops/RAD-280-pilot/serve.log`. It holds no tokens; check it first if
the health check fails.

For a full check, run an MCP `ping` with a bot's token (read from its file,
never echoed).

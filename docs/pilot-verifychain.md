# verifychain-ai pilot

Three bots share one BotOptimizer Memory project, `verifychain`. No other project is
granted, and there are no wildcards.

| Bot | Role | Can do |
|---|---|---|
| `pm-verifychain` | `owner` | everything, plus `delete` (archive) and `reindex` |
| `implementor-verifychain` | `write` | `get/list/search/history` plus `put/append` |
| `reviewer-verifychain` | `write` | `get/list/search/history` plus `put/append` (records its review findings) |

**Role decision:** the reviewer gets `write` so it can record findings in
BotOptimizer Memory (for example appending to a `review-*` record). No append-only role is
added. All writes are attributed via `updated_by` from the token, so reviewer
edits are always distinguishable; only the PM (`owner`) can archive or reindex.

## Granting (needs operator approval for real Drive)

On the BotOptimizer host, using the same env as `serve` (OAuth env plus `BOTOPTIMIZER_DRIVE_ROOT_ID`):

```sh
scripts/pilot_verifychain.sh                                   # dry run: shows the 3 grant commands
BOTOPTIMIZER_PILOT_ALLOW_DRIVE=1 scripts/pilot_verifychain.sh --apply
```

Each grant mints a fresh token and writes **only its SHA-256 hash** into
`_acl.json` (conditional on the Drive file version, retried on a concurrent
edit). It writes the raw token to `$PILOT_TOKEN_DIR/<bot>/botoptimizer.token`
(default `$HOME/.local/var/botoptimizer/pilot/<bot>/botoptimizer.token`) with mode
0600. That file must not already exist, and a symlink there is refused.
`O_NOFOLLOW` only checks the final path component, so the script creates each
per-bot directory with mode 0700 under `$PILOT_TOKEN_DIR`. If that dir or a
per-bot dir already exists, the script **refuses** (exit 3, never chmods)
unless it is a real directory owned by you with mode 0700. If the ACL write
fails after the token file was written, the CLI re-reads the ACL. If the new
hash is there, the grant succeeded. Otherwise the file is **kept** (Drive may
still apply the write late) and the CLI reports the outcome as unknown, giving
the file path. Check `botoptimizer acl list` later: if the bot is listed, the
token is live; if not, delete the file. Grant and revoke report success only
after a re-read shows the change. Only one `botoptimizer acl` run per vault at a
time on this host: a second run is refused while the per-vault ACL lock is held.

## Getting each bot its token (never via chat)

- Never paste a token into chat, Slack, an issue tracker, an email, a commit, or a prompt.
  Don't `cat` it into a terminal that is being recorded or logged.
- **Same host (the pilot setup):** all pilot bots run as the same Unix user, so
  there is no per-bot owner to `install -o` to, and Unix permissions can't
  separate one bot's token from another's. Keep each token where it was minted,
  `$PILOT_TOKEN_DIR/<bot>/botoptimizer.token` (file 0600, dir 0700), and have each
  bot's launcher read **only its own** file. Don't copy it anywhere else.
  Separation between bots is by convention plus the ACL roles, not by OS user.
- **Different host:** copy over an authenticated channel (`scp`/`rsync` over SSH)
  straight into the target path, then delete the staging copy. Don't use a
  pastebin, shared drive, or chat attachment.
- If a token might have been exposed, **rotate** it (see below) rather than
  reasoning about it.

## MCP config for each bot

BotOptimizer listens only on loopback: `http://127.0.0.1:8787/mcp` (Streamable HTTP).
The bot sends `Authorization: Bearer <token>`. Load the token from its file at
launch so it never sits in a config that gets committed or shared:

```sh
export BOTOPTIMIZER_TOKEN="$(cat "$PILOT_TOKEN_DIR/<bot>/botoptimizer.token")"   # in the bot's launcher, not a dotfile in git
```

```json
{
  "mcpServers": {
    "botoptimizer": {
      "url": "http://127.0.0.1:8787/mcp",
      "headers": { "Authorization": "Bearer ${BOTOPTIMIZER_TOKEN}" }
    }
  }
}
```

(The exact env interpolation syntax depends on the MCP client. If the client
can't interpolate, generate the config at launch into a mode-600 file outside
git.) Every call names `project: "verifychain"`, and other projects are denied.

## Revoke / rotate

```sh
botoptimizer acl list                                    # bots, roles, active/REVOKED; no hashes
botoptimizer acl revoke reviewer-verifychain             # stops working within 60s
botoptimizer acl remove reviewer-verifychain             # delete the entry entirely
botoptimizer acl grant reviewer-verifychain verifychain write --rotate \
  --token-out "$PILOT_TOKEN_DIR/reviewer-verifychain/botoptimizer.token.new"
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
`internal/drive` `TestACLAdminOnDrive`, `cmd/botoptimizer` `TestACLCommandMemory`).
On real Drive: the three pilot bots (pm-verifychain owner,
implementor-verifychain write, reviewer-verifychain write, on `verifychain`
only) were granted and smoke-tested. Each token did put, get and search on
verifychain and was denied on other projects. A throwaway bot, `qa-throwaway`,
was granted and revoked during QA, and is no longer in `_acl.json` (when
`botoptimizer acl remove qa-throwaway` ran it reported "no such bot", so the
entry had already been removed). `acl list` now shows exactly the three pilot bots, all
active.

## Server environment and restart (names only, never values)

`botoptimizer serve` reads these (via **`BOTOPTIMIZER_*`**):

| Variable | Needed for | Notes |
|---|---|---|
| `BOTOPTIMIZER_OAUTH_CLIENT_ID` | serve (drive) | host env secret |
| `BOTOPTIMIZER_OAUTH_CLIENT_SECRET` | serve (drive) | host env secret |
| `BOTOPTIMIZER_OAUTH_REFRESH_TOKEN` | serve (drive) | typically loaded from `$BOTOPTIMIZER_OAUTH_DIR/refresh_token` (0600) at launch |
| `BOTOPTIMIZER_DRIVE_ROOT_ID` | serve, acl (drive) | typically loaded from `$BOTOPTIMIZER_OAUTH_DIR/drive_root_id` |
| `BOTOPTIMIZER_ADDR` | serve | optional, default `127.0.0.1:8787`; non-loopback binds are refused |
| `BOTOPTIMIZER_BACKEND` | all | optional: `drive` (default) or `memory` (tests only) |
| `BOTOPTIMIZER_LOCK_FILE` | serve | optional instance-lock override |
| `BOTOPTIMIZER_ACL_FILE` | serve | optional local ACL file instead of Drive `_acl.json`; `acl` refuses to run while it's set |
| `BOTOPTIMIZER_ACL_LOCK_FILE` | acl | optional ACL-lock override |

`BOTOPTIMIZER_OAUTH_REDIRECT_URL` may be set for the one-time consent and code
exchange; `serve` doesn't read it.

The OAuth client id and secret live in the **host's env secrets**. They are
never in a `.env` file, the repo, or chat.

Restart: run the script from the repo root. Don't paste the steps into your
shell.

```sh
scripts/restart_serve.sh
```

It runs these steps in order and stops at the first failure:

1. **Binary.** It uses `$HOME/.local/bin/botoptimizer` (`BOTOPTIMIZER_BIN` overrides).
   If that's missing it runs `scripts/setup_box.sh` to build it, and fails if
   there is still no binary.
2. **Env.** It checks that `refresh_token` and `drive_root_id` exist and are
   non-empty in `$HOME/.config/botoptimizer` (`BOTOPTIMIZER_OAUTH_DIR` overrides),
   then loads them. It requires `BOTOPTIMIZER_OAUTH_CLIENT_ID`,
   `BOTOPTIMIZER_OAUTH_CLIENT_SECRET`, `BOTOPTIMIZER_OAUTH_REFRESH_TOKEN` and
   `BOTOPTIMIZER_DRIVE_ROOT_ID` to be non-empty. The client id and secret come from
   the host env secrets.
3. **Log dir.** `$HOME/.local/var/botoptimizer` (`BOTOPTIMIZER_LOG_DIR` overrides) is
   created with mode 0700. If it already exists, the script refuses unless the
   dir is yours and 0700.
4. **Stop.** It stops only a process whose command line is exactly
   `"$BIN" serve`, then polls for up to 15s (`STOP_TIMEOUT`). If the old
   process is still running, it fails **without** starting a new one.
5. **Start and check.** It starts `"$BIN" serve` on `127.0.0.1:8787`
   (`BOTOPTIMIZER_ADDR`). It then confirms the new process is alive and that an
   unauthenticated `POST /mcp` returns 401. It polls for up to 10s.

Steps 1 to 3 fail before anything is stopped, so a missing binary, env value
or log dir never takes down a running server. The script prints names only,
never values.

The server log (stdout and stderr) is appended to
`$BOTOPTIMIZER_LOG_DIR/serve.log`. It holds no tokens; check it first if the
health check fails.

For a full check, run an MCP `ping` with a bot's token (read from its file,
never echoed).

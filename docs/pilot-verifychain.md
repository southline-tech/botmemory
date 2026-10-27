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
edit). It writes the raw token to `~/.config/botvault/pilot/<bot>/botvault.token`
with mode 0600. That file must not already exist, and a symlink there is refused. `O_NOFOLLOW`
only checks the final path component, so the script creates each per-bot
directory with mode 0700 under your own config dir. If the ACL write fails
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
- **Same host (the pilot setup):** move the file into the bot's own config dir,
  owned by that bot's user, at mode 600:
  `install -m 600 -o <botuser> ~/.config/botvault/pilot/<bot>/botvault.token /home/<botuser>/.config/botvault/token`
  then delete the staging copy (`shred -u` or `rm`).
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
export BOTVAULT_TOKEN="$(cat ~/.config/botvault/token)"   # in the bot's launcher, not a dotfile in git
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
botvault acl grant reviewer-verifychain verifychain write --rotate \
  --token-out ~/.config/botvault/pilot/reviewer-verifychain/botvault.token.new
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
No real pilot tokens have been granted.

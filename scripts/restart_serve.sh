#!/usr/bin/env bash
# Restart the botMemory server on this box. Run it; don't paste it.
#
#   scripts/restart_serve.sh
#
# Order: binary present -> env and source files complete -> log dir safe ->
# stop the old server and wait for it to exit -> start -> wait for the
# "botmemory listening on" log line -> check the 401 health check. Any failure
# before the stop exits without touching the running server. Prints env names
# only, never values.
#
# Overrides (names only):
#   BOTMEMORY_BIN        binary (default $HOME/.local/bin/botmemory); if set, must be an absolute path
#   BOTMEMORY_OAUTH_DIR  dir with refresh_token + drive_root_id (default $HOME/.config/botmemory)
#   BOTMEMORY_LOG_DIR    log dir (default $HOME/.local/var/botmemory); log is serve.log
#   BOTMEMORY_ADDR       listen address (default 127.0.0.1:8787)
#   STOP_TIMEOUT        seconds to wait for the old process (default 15)
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

BIN=${BOTMEMORY_BIN:-$HOME/.local/bin/botmemory}
if [[ -n "${BOTMEMORY_BIN:-}" && "$BIN" != /* ]]; then
	die "BOTMEMORY_BIN must be an absolute path (got: $BIN)"
fi
OAUTH_DIR=${BOTMEMORY_OAUTH_DIR:-$HOME/.config/botmemory}
LOG_DIR=${BOTMEMORY_LOG_DIR:-$HOME/.local/var/botmemory}
ADDR=${BOTMEMORY_ADDR:-127.0.0.1:8787}
STOP_TIMEOUT=${STOP_TIMEOUT:-15}
die() { echo "restart_serve: $*" >&2; exit 1; }

# 1. binary
if [[ ! -x "$BIN" ]]; then
	BOTMEMORY_BIN="$BIN" scripts/setup_box.sh || die "setup_box.sh failed"
fi
[[ -x "$BIN" ]] || die "no botmemory binary at $BIN"

# 2. env and source files (all checked before anything is stopped)
for f in refresh_token drive_root_id; do
	[[ -f "$OAUTH_DIR/$f" && -s "$OAUTH_DIR/$f" ]] || die "missing or empty $OAUTH_DIR/$f"
done
BOTMEMORY_OAUTH_REFRESH_TOKEN="$(cat "$OAUTH_DIR/refresh_token")"
BOTMEMORY_DRIVE_ROOT_ID="$(cat "$OAUTH_DIR/drive_root_id")"
export BOTMEMORY_OAUTH_REFRESH_TOKEN BOTMEMORY_DRIVE_ROOT_ID
for v in BOTMEMORY_OAUTH_CLIENT_ID BOTMEMORY_OAUTH_CLIENT_SECRET BOTMEMORY_OAUTH_REFRESH_TOKEN BOTMEMORY_DRIVE_ROOT_ID; do
	[[ -n "${!v:-}" ]] || die "$v is missing or empty (client id/secret come from the box env secrets)"
done

# 3. log dir: create 0700, or require an existing one to be ours and 0700
if [[ ! -e "$LOG_DIR" && ! -L "$LOG_DIR" ]]; then
	(umask 077; mkdir -p "$LOG_DIR")
fi
[[ -d "$LOG_DIR" && ! -L "$LOG_DIR" ]] || die "$LOG_DIR is not a directory"
[[ "$(stat -c '%u %a' "$LOG_DIR")" == "$(id -u) 700" ]] || die "$LOG_DIR must be owned by uid $(id -u) with mode 700; fix it by hand"
LOG="$LOG_DIR/serve.log"

# 4. stop exactly "$BIN serve" and wait for it to exit
PAT="$BIN serve"
if pgrep -f -x "$PAT" >/dev/null; then
	pkill -f -x "$PAT" || true
	for ((i = 0; i < STOP_TIMEOUT * 2; i++)); do
		pgrep -f -x "$PAT" >/dev/null || break
		sleep 0.5
	done
	if pgrep -f -x "$PAT" >/dev/null; then
		die "old server still running after ${STOP_TIMEOUT}s; not starting a new one"
	fi
fi

# 5. start, then wait for listening, then check the process and health (401)
export BOTMEMORY_ADDR="$ADDR"
nohup "$BIN" serve >>"$LOG" 2>&1 &
pid=$!
LISTEN_TIMEOUT=${LISTEN_TIMEOUT:-30}
listened=0
for ((i = 0; i < LISTEN_TIMEOUT * 2; i++)); do
	sleep 0.5
	kill -0 "$pid" 2>/dev/null || die "server exited at startup; see $LOG"
	if grep -q 'botmemory listening on http://' "$LOG" 2>/dev/null; then
		listened=1
		break
	fi
done
(( listened )) || die "timed out after ${LISTEN_TIMEOUT}s waiting for 'botmemory listening on' in $LOG"
code=000
for ((i = 0; i < 20; i++)); do
	sleep 0.5
	kill -0 "$pid" 2>/dev/null || die "server exited after listen; see $LOG"
	code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "http://$ADDR/mcp" || true)
	[[ "$code" == 401 ]] && break
done
[[ "$code" == 401 ]] || die "health check: POST /mcp returned $code, want 401; see $LOG"
echo "restart_serve: botmemory serve running (pid $pid) on $ADDR; listening OK; health 401 OK; log $LOG"

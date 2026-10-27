#!/usr/bin/env bash
# RAD-280 botVault: rebuild the server on a fresh/wiped box.
#
#   scripts/setup_box.sh            # install Go if needed, build, check env + root id
#   scripts/setup_box.sh --test     # ...and run every package's tests (-race, one at a time)
#
# Contains no secrets and never prints secret values: env vars are reported by
# NAME only (set / MISSING). Secrets come from your environment or a
# gitignored .env (mode 600) that you restore yourself.
#
# Env (all optional):
#   BOTVAULT_BIN           output binary   (default $HOME/.local/bin/botvault)
#   BOTVAULT_ROOT_ID_FILE  root id file    (default $HOME/.config/botvault/drive_root_id)
#   GO_INSTALL_DIR         where Go goes   (default $HOME/.local/go)
set -euo pipefail

cd "$(dirname "$0")/.."
REPO=$(pwd)
RUN_TESTS=0
[[ "${1:-}" == "--test" ]] && RUN_TESTS=1

BIN=${BOTVAULT_BIN:-$HOME/.local/bin/botvault}
ROOT_ID_FILE=${BOTVAULT_ROOT_ID_FILE:-$HOME/.config/botvault/drive_root_id}
GO_DIR=${GO_INSTALL_DIR:-$HOME/.local/go}
WANT_GO=$(awk '/^go /{print $2}' go.mod)
fail=0

say() { printf '%s\n' "$*"; }

# 1. Go (version from go.mod), verified against go.dev's published sha256.
have_go() {
	command -v go >/dev/null || return 1
	local v; v=$(go env GOVERSION 2>/dev/null | sed 's/^go//')
	[[ "$(printf '%s\n%s\n' "$WANT_GO" "$v" | sort -V | head -1)" == "$WANT_GO" ]]
}
if [[ -x "$GO_DIR/bin/go" ]]; then export PATH="$GO_DIR/bin:$PATH"; fi
if have_go; then
	say "go: $(go env GOVERSION) (need >= go$WANT_GO) ok"
else
	say "go: installing go$WANT_GO into $GO_DIR"
	arch=$(uname -m); case "$arch" in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; esac
	file="go$WANT_GO.linux-$arch.tar.gz"
	sum=$(curl -fsSL 'https://go.dev/dl/?mode=json&include=all' |
		python3 -c 'import sys,json; f=sys.argv[1]; print(next(x["sha256"] for r in json.load(sys.stdin) for x in r["files"] if x["filename"]==f))' "$file")
	tmp=$(mktemp -d "$REPO/.go-dl.XXXXXX"); trap 'rm -rf "$tmp"' EXIT
	curl -fsSL -o "$tmp/$file" "https://go.dev/dl/$file"
	echo "$sum  $tmp/$file" | sha256sum -c --quiet -
	rm -rf "$GO_DIR"; mkdir -p "$(dirname "$GO_DIR")"
	tar -C "$tmp" -xzf "$tmp/$file" && mv "$tmp/go" "$GO_DIR"
	export PATH="$GO_DIR/bin:$PATH"
	have_go || { say "go: install failed"; exit 1; }
	say "go: $(go env GOVERSION) installed; add $GO_DIR/bin to PATH in your shell profile"
fi

# 2. Build.
mkdir -p "$(dirname "$BIN")"
go build -trimpath -o "$BIN" ./cmd/botvault
say "build: $BIN ($(git rev-parse --short HEAD 2>/dev/null || echo '?'))"

# 3. Optional tests, one package at a time (the box is shared).
if (( RUN_TESTS )); then
	for p in $(go list ./...); do go test -count=1 -race "$p"; done
	go vet ./...
fi

# 4. Env: load a gitignored .env if present (refuse if group/world-readable).
if [[ -f .env ]]; then
	perm=$(stat -c %a .env)
	if [[ "$perm" != 600 && "$perm" != 400 ]]; then
		say ".env: mode $perm; run: chmod 600 .env"; fail=1
	else
		set -a; . ./.env; set +a; say ".env: loaded (mode $perm)"
	fi
fi
check() { if [[ -n "${!1:-}" ]]; then say "env $1: set"; else say "env $1: MISSING"; fail=1; fi; }
if [[ "${BOTVAULT_BACKEND:-drive}" == "drive" ]]; then
	if [[ -n "${GOOGLE_APPLICATION_CREDENTIALS:-}" && -z "${BOTVAULT_OAUTH_REFRESH_TOKEN:-}" ]]; then
		say "env GOOGLE_APPLICATION_CREDENTIALS: set (ADC)"
	else
		check BOTVAULT_OAUTH_CLIENT_ID; check BOTVAULT_OAUTH_CLIENT_SECRET; check BOTVAULT_OAUTH_REFRESH_TOKEN
	fi
else
	say "env BOTVAULT_BACKEND=${BOTVAULT_BACKEND} (no Drive credentials needed)"
fi

# 5. Drive root id: env wins; otherwise the root id file. (Not a secret.)
if [[ "${BOTVAULT_BACKEND:-drive}" == "drive" ]]; then
	if [[ -n "${BOTVAULT_DRIVE_ROOT_ID:-}" ]]; then
		if [[ ! -s "$ROOT_ID_FILE" ]]; then
			mkdir -p "$(dirname "$ROOT_ID_FILE")"; umask 077; printf '%s\n' "$BOTVAULT_DRIVE_ROOT_ID" >"$ROOT_ID_FILE"
			say "root id: saved to $ROOT_ID_FILE"
		elif [[ "$(tr -d '[:space:]' <"$ROOT_ID_FILE")" != "$BOTVAULT_DRIVE_ROOT_ID" ]]; then
			say "root id: env and $ROOT_ID_FILE DIFFER; refusing to guess which vault you mean"; fail=1
		else
			say "root id: env matches $ROOT_ID_FILE"
		fi
	elif [[ -s "$ROOT_ID_FILE" ]]; then
		say "root id: from $ROOT_ID_FILE; run with: export BOTVAULT_DRIVE_ROOT_ID=\"\$(cat $ROOT_ID_FILE)\""
	else
		say "root id: MISSING; run \`$BIN bootstrap\` once (prints BOTVAULT_DRIVE_ROOT_ID), then save it to $ROOT_ID_FILE"; fail=1
	fi
fi

if (( fail )); then say "setup: INCOMPLETE (see MISSING/refusing lines above)"; exit 1; fi
say "setup: ok; start with: BOTVAULT_DRIVE_ROOT_ID=\"\$(cat $ROOT_ID_FILE)\" $BIN serve"

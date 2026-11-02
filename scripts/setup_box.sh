#!/usr/bin/env bash
# Rebuild the BotOptimizer server on a fresh/wiped box.
#
#   scripts/setup_box.sh            # install Go if needed, build, check env + root id
#   scripts/setup_box.sh --test     # ...and run every package's tests (-race, one at a time)
#
# Contains no secrets and never prints secret values: env vars are reported by
# NAME only (set / MISSING). Secrets come from your environment or a
# gitignored .env (mode 600) that you restore yourself.
#
# Env (all optional):
#   BOTOPTIMIZER_BIN           output binary   (default $HOME/.local/bin/botoptimizer); if set, must be absolute
#   BOTOPTIMIZER_ROOT_ID_FILE  root id file    (default $HOME/.config/botoptimizer/drive_root_id)
#   GO_INSTALL_DIR         where Go goes   (default $HOME/.local/go)
#
# Disposable Drive root for smoke/tests: run `botoptimizer bootstrap --test-folder`
# (or BOTOPTIMIZER_TEST_FOLDER=1) and save the printed BOTOPTIMIZER_DRIVE_ROOT_ID;
# set BOTOPTIMIZER_SMOKE_TEST_ROOT=1 when pointing scripts/smoke_drive.py at it.
set -euo pipefail

say() { printf '%s\n' "$*"; }

cd "$(dirname "$0")/.."
REPO=$(pwd)
RUN_TESTS=0
[[ "${1:-}" == "--test" ]] && RUN_TESTS=1

BIN=${BOTOPTIMIZER_BIN:-$HOME/.local/bin/botoptimizer}
if [[ -n "${BOTOPTIMIZER_BIN:-}" && "$BIN" != /* ]]; then
	say "BOTOPTIMIZER_BIN must be an absolute path (got: $BIN)"; exit 1
fi
ROOT_ID_FILE=${BOTOPTIMIZER_ROOT_ID_FILE:-$HOME/.config/botoptimizer/drive_root_id}
GO_DIR=${GO_INSTALL_DIR:-$HOME/.local/go}
WANT_GO=$(awk '/^go /{print $2}' go.mod)
fail=0

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
	# Guard the rm -rf: never an empty path, /, $HOME, or a non-absolute path.
	[[ "$GO_DIR" == /* ]] || { say "go: GO_INSTALL_DIR must be an absolute path"; exit 1; }
	go_dir_real=$(realpath -m -- "$GO_DIR")
	case "$go_dir_real" in
	"" | / | "$(realpath -m -- "$HOME")" | /usr | /usr/local | /home)
		say "go: refusing to replace GO_INSTALL_DIR=$GO_DIR"; exit 1 ;;
	esac
	arch=$(uname -m); case "$arch" in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; esac
	file="go$WANT_GO.linux-$arch.tar.gz"
	sum=$(curl -fsSL 'https://go.dev/dl/?mode=json&include=all' |
		python3 -c 'import sys,json; f=sys.argv[1]; print(next(x["sha256"] for r in json.load(sys.stdin) for x in r["files"] if x["filename"]==f))' "$file")
	tmp=$(mktemp -d "$REPO/.go-dl.XXXXXX"); trap 'rm -rf "$tmp"' EXIT
	curl -fsSL -o "$tmp/$file" "https://go.dev/dl/$file"
	echo "$sum  $tmp/$file" | sha256sum -c --quiet -
	rm -rf -- "$go_dir_real"; mkdir -p "$(dirname "$go_dir_real")"
	tar -C "$tmp" -xzf "$tmp/$file" && mv "$tmp/go" "$go_dir_real"
	export PATH="$GO_DIR/bin:$PATH"
	have_go || { say "go: install failed"; exit 1; }
	say "go: $(go env GOVERSION) installed; add $GO_DIR/bin to PATH in your shell profile"
fi

# 2. Build.
mkdir -p "$(dirname "$BIN")"
go build -trimpath -o "$BIN" ./cmd/botoptimizer
say "build: $BIN ($(git rev-parse --short HEAD 2>/dev/null || echo '?'))"

# 3. Optional tests, one package at a time (the box is shared).
if (( RUN_TESTS )); then
	for p in $(go list ./...); do go test -count=1 -race "$p"; done
	go vet ./...
fi

# 4. Env: load a gitignored .env if present (refuse if group/world-readable).
if [[ -f .env ]]; then
	perm=$(stat -c %a .env)
	if [[ -L .env || "$(stat -c %u .env)" != "$(id -u)" ]]; then
		say ".env: not a regular file owned by you; refusing to load it"; fail=1
	elif [[ "$perm" != 600 && "$perm" != 400 ]]; then
		say ".env: mode $perm; run: chmod 600 .env"; fail=1
	else
		set -a; . ./.env; set +a; say ".env: loaded (mode $perm)"
	fi
fi
check() { if [[ -n "${!1:-}" ]]; then say "env $1: set"; else say "env $1: MISSING"; fail=1; fi; }
if [[ "${BOTOPTIMIZER_BACKEND:-drive}" == "drive" ]]; then
	if [[ -n "${GOOGLE_APPLICATION_CREDENTIALS:-}" && -z "${BOTOPTIMIZER_OAUTH_REFRESH_TOKEN:-}" ]]; then
		say "env GOOGLE_APPLICATION_CREDENTIALS: set (ADC)"
	else
		check BOTOPTIMIZER_OAUTH_CLIENT_ID; check BOTOPTIMIZER_OAUTH_CLIENT_SECRET; check BOTOPTIMIZER_OAUTH_REFRESH_TOKEN
	fi
else
	say "env BOTOPTIMIZER_BACKEND=${BOTOPTIMIZER_BACKEND} (no Drive credentials needed)"
fi

# 5. Drive root id: env wins; otherwise the root id file. (Not a secret.)
if [[ "${BOTOPTIMIZER_BACKEND:-drive}" == "drive" ]]; then
	if [[ -n "${BOTOPTIMIZER_DRIVE_ROOT_ID:-}" ]]; then
		if [[ ! -s "$ROOT_ID_FILE" ]]; then
			mkdir -p "$(dirname "$ROOT_ID_FILE")"; umask 077; printf '%s\n' "$BOTOPTIMIZER_DRIVE_ROOT_ID" >"$ROOT_ID_FILE"
			say "root id: saved to $ROOT_ID_FILE"
		elif [[ "$(tr -d '[:space:]' <"$ROOT_ID_FILE")" != "$BOTOPTIMIZER_DRIVE_ROOT_ID" ]]; then
			say "root id: env and $ROOT_ID_FILE DIFFER; refusing to guess which vault you mean"; fail=1
		else
			say "root id: env matches $ROOT_ID_FILE"
		fi
	elif [[ -s "$ROOT_ID_FILE" ]]; then
		say "root id: from $ROOT_ID_FILE; run with: export BOTOPTIMIZER_DRIVE_ROOT_ID=\"\$(cat $ROOT_ID_FILE)\""
	else
		say "root id: MISSING; run \`$BIN bootstrap\` once (prints BOTOPTIMIZER_DRIVE_ROOT_ID), or \`$BIN bootstrap --test-folder\` for a throwaway smoke root; then save it to $ROOT_ID_FILE"; fail=1
	fi
fi

if (( fail )); then say "setup: INCOMPLETE (see MISSING/refusing lines above)"; exit 1; fi
say "setup: ok; start with: BOTOPTIMIZER_DRIVE_ROOT_ID=\"\$(cat $ROOT_ID_FILE)\" $BIN serve"

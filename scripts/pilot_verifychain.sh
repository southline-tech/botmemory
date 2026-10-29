#!/usr/bin/env bash
# Grant the three verifychain-ai pilot bots on project "verifychain".
#
#   scripts/pilot_verifychain.sh            # DRY RUN: print the commands only
#   scripts/pilot_verifychain.sh --apply    # run them against the configured backend
#
# Real Drive grants additionally need BOTMEMORY_PILOT_ALLOW_DRIVE=1, and only
# with operator approval. Tokens are written to
# $PILOT_TOKEN_DIR/<bot>/botmemory.token (mode 0600) and NEVER printed.
set -euo pipefail
BIN=${BOTMEMORY_BIN:-botmemory}
DIR=${PILOT_TOKEN_DIR:-$HOME/.local/var/botmemory/pilot}
PROJECT=verifychain
BOTS=("pm-verifychain owner" "implementor-verifychain write" "reviewer-verifychain write")

apply=0; [[ "${1:-}" == "--apply" ]] && apply=1
if (( apply )) && [[ "${BOTMEMORY_BACKEND:-drive}" == "drive" && "${BOTMEMORY_PILOT_ALLOW_DRIVE:-}" != 1 ]]; then
	echo "refusing: this would grant REAL tokens in Drive. Set BOTMEMORY_PILOT_ALLOW_DRIVE=1 after operator approval." >&2
	exit 2
fi
# Refuse (never chmod) an existing token dir that isn't a real dir, 0700, owned by us.
check_dir() {
	local d=$1
	[[ -e "$d" || -L "$d" ]] || return 0
	if [[ -L "$d" || ! -d "$d" ]]; then echo "refusing: $d is not a directory" >&2; exit 3; fi
	local st; st=$(stat -c '%u %a' "$d")
	if [[ "$st" != "$(id -u) 700" ]]; then
		echo "refusing: $d must be owned by uid $(id -u) with mode 700 (is: uid/mode $st); fix it by hand" >&2; exit 3
	fi
}
check_dir "$DIR"
for e in "${BOTS[@]}"; do read -r b _ <<<"$e"; check_dir "$DIR/$b"; done
for e in "${BOTS[@]}"; do
	read -r bot role <<<"$e"
	out="$DIR/$bot/botmemory.token"
	cmd=("$BIN" acl grant "$bot" "$PROJECT" "$role" --token-out "$out")
	if (( apply )); then
		(umask 077; mkdir -p "$DIR/$bot")
		"${cmd[@]}"
	else
		printf 'would run: mkdir -m 700 -p %q && %s\n' "$DIR/$bot" "${cmd[*]}"
	fi
done
(( apply )) && "$BIN" acl list
exit 0

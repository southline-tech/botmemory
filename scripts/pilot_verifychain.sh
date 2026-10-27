#!/usr/bin/env bash
# RAD-280 M3: grant the three verifychain-ai pilot bots on project "verifychain".
#
#   scripts/pilot_verifychain.sh            # DRY RUN: print the commands only
#   scripts/pilot_verifychain.sh --apply    # run them against the configured backend
#
# Real Drive grants additionally need BOTVAULT_PILOT_ALLOW_DRIVE=1, and only
# with Brett's approval. Tokens are written to
# $PILOT_TOKEN_DIR/<bot>/botvault.token (mode 0600) and NEVER printed.
set -euo pipefail
BIN=${BOTVAULT_BIN:-botvault}
DIR=${PILOT_TOKEN_DIR:-$HOME/.config/botvault/pilot}
PROJECT=verifychain
BOTS=("pm-verifychain owner" "implementor-verifychain write" "reviewer-verifychain read")

apply=0; [[ "${1:-}" == "--apply" ]] && apply=1
if (( apply )) && [[ "${BOTVAULT_BACKEND:-drive}" == "drive" && "${BOTVAULT_PILOT_ALLOW_DRIVE:-}" != 1 ]]; then
	echo "refusing: this would grant REAL tokens in Drive. Needs Brett's approval, then BOTVAULT_PILOT_ALLOW_DRIVE=1." >&2
	exit 2
fi
for e in "${BOTS[@]}"; do
	read -r bot role <<<"$e"
	out="$DIR/$bot/botvault.token"
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

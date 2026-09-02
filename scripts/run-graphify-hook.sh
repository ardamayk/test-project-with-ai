#!/bin/sh
set -eu

[ "${GRAPHIFY_SKIP_HOOK:-0}" != "1" ] || exit 0
[ -d graphify-out ] || exit 0
command -v graphify >/dev/null 2>&1 || exit 0

case "${1:-}" in
	post-commit)
		;;
	post-checkout)
		[ "${4:-0}" = "1" ] || exit 0
		;;
	*)
		echo "Usage: $0 post-commit | post-checkout PREV_HEAD NEW_HEAD BRANCH_SWITCH" >&2
		exit 2
		;;
esac

graphifyLog="$(git rev-parse --git-path graphify-hook.log)"
echo "[graphify] Updating graph in background (log: $graphifyLog)"
graphify update . >>"$graphifyLog" 2>&1 &

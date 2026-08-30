#!/usr/bin/env bash
set -euo pipefail

readonly SERVER_ADDRESS="${SERVER_ADDR:-127.0.0.1:8090}"
readonly SERVER_HEALTH_URL="${EARTHLY_SERVER_HEALTH_URL:-http://$SERVER_ADDRESS/api/v1/health}"
readonly SERVER_STARTUP_ATTEMPTS=100
readonly SERVER_STARTUP_DELAY_SECONDS=0.1

serverPid=""

isMusicServerReady() {
	local response
	response="$(curl --silent --show-error --max-time 1 "$SERVER_HEALTH_URL" 2>/dev/null)" || return 1
	[[ "$response" == *'"status":"ok"'* && "$response" == *'"api.v1"'* ]]
}

stopOwnedServer() {
	local exitCode=$?
	trap - EXIT INT TERM
	if [[ -n "$serverPid" ]] && kill -0 -- "-$serverPid" 2>/dev/null; then
		kill -- "-$serverPid" 2>/dev/null || true
		wait "$serverPid" 2>/dev/null || true
	fi
	exit "$exitCode"
}

startMusicServer() {
	local serverMode="$1"
	case "$serverMode" in
		development)
			setsid bash -c 'cd server && exec go run ./cmd/server' &
			;;
		built)
			setsid bash -c 'cd server && exec ../bin/server' &
			;;
		*)
			echo "Unknown Music Server mode: $serverMode" >&2
			exit 2
			;;
	esac
	serverPid=$!
	trap stopOwnedServer EXIT INT TERM
}

waitForMusicServer() {
	local attempt
	for ((attempt = 1; attempt <= SERVER_STARTUP_ATTEMPTS; attempt += 1)); do
		if isMusicServerReady; then
			return 0
		fi
		if ! kill -0 "$serverPid" 2>/dev/null; then
			wait "$serverPid" || true
			echo "Music Server stopped before becoming ready. Check its output and configured port." >&2
			return 1
		fi
		sleep "$SERVER_STARTUP_DELAY_SECONDS"
	done
	echo "Music Server did not become ready at $SERVER_HEALTH_URL." >&2
	return 1
}

if [[ $# -lt 2 ]]; then
	echo "Usage: $0 <development|built> <client command...>" >&2
	exit 2
fi

serverMode="$1"
shift

if isMusicServerReady; then
	echo "Reusing Music Server at $SERVER_HEALTH_URL."
else
	startMusicServer "$serverMode"
	waitForMusicServer
fi

"$@"

#!/usr/bin/env bash
set -euo pipefail
# The server outlives individual test runs, so it must not inherit their TMPDIR.
export TMPDIR="${XDG_CACHE_HOME:-$HOME/.cache}/earthly-audio/sccache-tmp"
current="$TMPDIR"
while [[ "$current" != / ]]; do
  [[ ! -L "$current" ]] || { echo "Refusing symbolic compiler temporary path: $current" >&2; exit 1; }
  current="${current%/*}"
  current="${current:-/}"
done
mkdir -p "$TMPDIR"
exec "${EARTHLY_SCCACHE_BINARY:?Compiler cache executable is missing; run through Mise}" "$@"

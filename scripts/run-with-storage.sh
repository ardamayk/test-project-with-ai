#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIRECTORY="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIRECTORY/storage-env.sh"
kind="${1:?Storage command kind is required}"
shift
if [[ "$EARTHLY_STORAGE_RESOLVED_MODE" != local ]]; then exec "$@"; fi

if [[ "${EARTHLY_LOCKED_WORKTREE:-}" != "$EARTHLY_WORKTREE_ROOT" ]]; then
  lockPath="$(node "$SCRIPT_DIRECTORY/storage.mjs" lock-path)"
  export EARTHLY_LOCKED_WORKTREE="$EARTHLY_WORKTREE_ROOT"
  exec flock --close --shared "$lockPath" bash "$SCRIPT_DIRECTORY/run-with-storage.sh" "$kind" "$@"
fi
node "$SCRIPT_DIRECTORY/storage.mjs" prepare
case "$kind" in
  web-build|docs-build|server-build)
    artifactMarker="EARTHLY_LOCKED_${kind//-/_}"
    if [[ "${!artifactMarker:-}" != "$EARTHLY_WORKTREE_ROOT" ]]; then
      artifactLock="$EARTHLY_CLONE_ROOT/locks/$EARTHLY_WORKTREE_ID-$kind.lock"
      node --input-type=module -e 'const { assertNoSymlinks } = await import(process.argv[1]); assertNoSymlinks(process.argv[2]);' "$SCRIPT_DIRECTORY/storage-paths.mjs" "$artifactLock"
      export "$artifactMarker=$EARTHLY_WORKTREE_ROOT"
      exec flock --close --exclusive "$artifactLock" bash "$SCRIPT_DIRECTORY/run-with-storage.sh" "$kind" "$@"
    fi
    ;;
esac
if [[ "$kind" == browser && "${EARTHLY_BROWSER_LOCKED:-0}" != 1 ]]; then
  browserLocks="${XDG_CACHE_HOME:-$HOME/.cache}/earthly-audio/locks"
  node --input-type=module -e 'const { assertNoSymlinks } = await import(process.argv[1]); assertNoSymlinks(process.argv[2]);' "$SCRIPT_DIRECTORY/storage-paths.mjs" "$browserLocks/browser.lock"
  mkdir -p "$browserLocks"
  export EARTHLY_BROWSER_LOCKED=1
  exec flock --close --exclusive "$browserLocks/browser.lock" bash "$SCRIPT_DIRECTORY/run-with-storage.sh" "$kind" "$@"
fi

if [[ "$kind" == test || "$kind" == browser || "$kind" == go-test ]]; then
  if [[ -z "${EARTHLY_RUN_DIR:-}" ]]; then
    runPaths="$(node "$SCRIPT_DIRECTORY/storage.mjs" run)"
    export EARTHLY_RUN_DIR="${runPaths%%$'\n'*}"
    export EARTHLY_TMP_ALIAS="${runPaths#*$'\n'}"
    export EARTHLY_STORAGE_RUN_PID="$$"
    export EARTHLY_RUN_WORKTREE="$EARTHLY_WORKTREE_ROOT"
    export EARTHLY_PREVIOUS_TMPDIR_SET="${TMPDIR+1}" EARTHLY_PREVIOUS_TMPDIR="${TMPDIR:-}"
    export TMPDIR="$EARTHLY_RUN_DIR/tmp"
    trap 'node "$SCRIPT_DIRECTORY/storage.mjs" finish-run "$EARTHLY_RUN_DIR"' EXIT
  fi
fi
if [[ "$kind" == go-test ]]; then
  # Go records TMPDIR in test-result cache keys; t.TempDir isolates individual tests.
  node --input-type=module -e 'const { assertNoSymlinks } = await import(process.argv[1]); assertNoSymlinks(process.argv[2]);' "$SCRIPT_DIRECTORY/storage-paths.mjs" "$EARTHLY_GO_TEST_TMP"
  mkdir -p "$EARTHLY_GO_TEST_TMP"
  export TMPDIR="$EARTHLY_GO_TEST_TMP"
  export GOTMPDIR="${GOTMPDIR:-$EARTHLY_GO_TEST_TMP}"
fi
if [[ "$kind" == desktop && -z "${RUSTC_WRAPPER:-}" ]]; then
  echo "Compiler cache inactive; run 'mise run cache:setup' to install pinned sccache." >&2
fi
"$@"

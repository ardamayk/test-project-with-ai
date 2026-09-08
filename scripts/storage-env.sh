#!/usr/bin/env bash
# Mise sources this file before tasks. Resolve paths without writing to disk.

storageSetDefault() {
  local name="$1" value="$2" marker="EARTHLY_DEFAULT_$1"
  if [[ ! -v "$name" ]]; then
    export "$name=$value" "$marker=$value"
  fi
}

storageClearDefaults() {
  local marker name
  for marker in ${!EARTHLY_DEFAULT_@}; do
    name="${marker#EARTHLY_DEFAULT_}"
    if [[ "${!name-}" == "${!marker}" ]]; then unset "$name"; fi
    unset "$marker"
  done
  unset EARTHLY_CLONE_ROOT EARTHLY_WORKTREE_ROOT EARTHLY_CHECKOUT_ROOT
  unset EARTHLY_CLONE_ID EARTHLY_WORKTREE_ID EARTHLY_GIT_COMMON_DIR
}

storageClearRun() {
  if [[ -n "${EARTHLY_RUN_WORKTREE:-}" ]]; then
    if [[ "${TMPDIR:-}" == "${EARTHLY_TMP_ALIAS:-}" || "${TMPDIR:-}" == "${EARTHLY_RUN_DIR:-}/tmp" || "${TMPDIR:-}" == "$EARTHLY_RUN_WORKTREE/cache/go-test-tmp" ]]; then
      if [[ "${EARTHLY_PREVIOUS_TMPDIR_SET:-0}" == 1 ]]; then
        export TMPDIR="$EARTHLY_PREVIOUS_TMPDIR"
      else
        unset TMPDIR
      fi
    fi
    if [[ "${GOTMPDIR:-}" == "${EARTHLY_RUN_DIR:-}/tmp" || "${GOTMPDIR:-}" == "$EARTHLY_RUN_WORKTREE/cache/go-test-tmp" ]]; then unset GOTMPDIR; fi
    unset EARTHLY_RUN_DIR EARTHLY_TMP_ALIAS EARTHLY_STORAGE_RUN_PID EARTHLY_RUN_WORKTREE
    unset EARTHLY_PREVIOUS_TMPDIR EARTHLY_PREVIOUS_TMPDIR_SET
  fi
}

storageAddToolShims() {
  local tool executable variable
  for tool in cargo pnpm go; do
    variable="EARTHLY_REAL_${tool^^}"
    while IFS= read -r executable; do
      if [[ "$executable" != */scripts/storage-bin/* && "$executable" != */mise/shims/* ]]; then
        export "$variable=$executable"
        break
      fi
    done < <(type -aP "$tool" || true)
  done
  if [[ "$PATH" != "$EARTHLY_CHECKOUT_ROOT/scripts/storage-bin:"* ]]; then
    export PATH="$EARTHLY_CHECKOUT_ROOT/scripts/storage-bin:$PATH"
  fi
}

storageResolve() {
  storageClearDefaults
  local mode="${EARTHLY_STORAGE_MODE:-auto}"
  if [[ "$mode" == auto ]]; then
    if [[ "${GITHUB_ACTIONS:-false}" == true || "${CI:-false}" == true || "${CI:-0}" == 1 ]]; then
      mode=ci
    else
      mode=local
    fi
  fi
  case "$mode" in local|ci|clean-room|legacy) ;; *)
    echo "Invalid EARTHLY_STORAGE_MODE: $mode" >&2; return 2 ;;
  esac
  export EARTHLY_STORAGE_RESOLVED_MODE="$mode"
  if [[ "$mode" != local ]]; then storageClearRun; return 0; fi

  local checkout common root cloneId worktreeId
  checkout="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)" || return
  common="$(git -C "$checkout" rev-parse --path-format=absolute --git-common-dir 2>/dev/null)" || {
    echo "Local storage requires a Git checkout; use EARTHLY_STORAGE_MODE=legacy outside Git." >&2
    return 2
  }
  common="$(realpath -- "$common")" || return
  root="$(realpath -m -- "${EARTHLY_CACHE_ROOT:-${XDG_CACHE_HOME:-$HOME/.cache}/earthly-audio}")" || return
  cloneId="$(printf '%s' "$common" | sha256sum)"; cloneId="${cloneId%% *}"
  worktreeId="$(printf '%s' "$checkout" | sha256sum)"; worktreeId="${worktreeId%% *}"
  export EARTHLY_CHECKOUT_ROOT="$checkout" EARTHLY_GIT_COMMON_DIR="$common"
  export EARTHLY_CLONE_ID="$cloneId" EARTHLY_WORKTREE_ID="$worktreeId"
  export EARTHLY_CLONE_ROOT="$root/clones/$cloneId"
  export EARTHLY_WORKTREE_ROOT="$EARTHLY_CLONE_ROOT/worktrees/$worktreeId"
  if [[ -n "${EARTHLY_RUN_WORKTREE:-}" && "$EARTHLY_RUN_WORKTREE" != "$EARTHLY_WORKTREE_ROOT" ]]; then storageClearRun; fi
  storageAddToolShims
  storageSetDefault CARGO_TARGET_DIR "$EARTHLY_WORKTREE_ROOT/build/cargo-target"
  storageSetDefault TURBO_CACHE_DIR "$EARTHLY_CLONE_ROOT/shared/turbo"
  storageSetDefault EARTHLY_WEB_DIST "$EARTHLY_WORKTREE_ROOT/build/web"
  storageSetDefault EARTHLY_DOCS_DIST "$EARTHLY_WORKTREE_ROOT/build/docs"
  storageSetDefault EARTHLY_SERVER_BINARY "$EARTHLY_WORKTREE_ROOT/build/bin/server"
  storageSetDefault EARTHLY_MPV_DIRECTORY "$EARTHLY_WORKTREE_ROOT/build/sidecar"
  storageSetDefault EARTHLY_VITE_CACHE "$EARTHLY_WORKTREE_ROOT/cache/vite/node_modules/.vite"
  storageSetDefault EARTHLY_GO_TEST_TMP "$EARTHLY_WORKTREE_ROOT/cache/go-test-tmp"
  storageSetDefault EARTHLY_RUN_ROOT "$EARTHLY_WORKTREE_ROOT/runs"

  local compilerPath="${XDG_CACHE_HOME:-$HOME/.cache}/earthly-audio/tools/sccache.path"
  local compiler=""
  if [[ -r "$compilerPath" ]]; then IFS= read -r compiler < "$compilerPath"; fi
  if [[ -z "${RUSTC_WRAPPER:-}" && -n "$compiler" && -x "$compiler" ]]; then
    storageSetDefault EARTHLY_SCCACHE_BINARY "$compiler"
    storageSetDefault RUSTC_WRAPPER "$checkout/scripts/run-sccache.sh"
    storageSetDefault CARGO_INCREMENTAL 0
    storageSetDefault SCCACHE_CACHE_SIZE 10G
  fi
}

storageResolve

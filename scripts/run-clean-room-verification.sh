#!/usr/bin/env bash
set -euo pipefail

TEMPORARY_PARENT="${TMPDIR:-/tmp}"
CLEAN_ROOM_ROOT="$(mktemp -d "${TEMPORARY_PARENT%/}/clean-room.XXXXXX")"
CONTAINER_BUILDER_NAME="navidrome-clean-room-${PPID}-$$"
IS_CONTAINER_BUILDER_CREATED=false

cleanup() {
  cleanupStatus=$?
  trap - EXIT
  if [[ "${IS_CONTAINER_BUILDER_CREATED}" == "true" ]]; then
    if ! docker buildx rm --force "${CONTAINER_BUILDER_NAME}" >/dev/null; then
      echo "Failed to remove temporary Buildx builder: ${CONTAINER_BUILDER_NAME}" >&2
      [[ "${cleanupStatus}" -ne 0 ]] || cleanupStatus=1
    fi
  fi
  if [[ -n "${CLEAN_ROOM_ROOT}" && -d "${CLEAN_ROOM_ROOT}" && "${CLEAN_ROOM_ROOT}" == "${TEMPORARY_PARENT%/}/clean-room."* ]]; then
    if ! rm -rf -- "${CLEAN_ROOM_ROOT}"; then
      echo "Failed to remove clean-room directory: ${CLEAN_ROOM_ROOT}" >&2
      [[ "${cleanupStatus}" -ne 0 ]] || cleanupStatus=1
    fi
  fi
  exit "${cleanupStatus}"
}

trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

export GOCACHE="${CLEAN_ROOM_ROOT}/cache/go-build"
export GOMODCACHE="${CLEAN_ROOM_ROOT}/cache/go-modules"
export GOLANGCI_LINT_CACHE="${CLEAN_ROOM_ROOT}/cache/golangci-lint"
export CARGO_HOME="${CLEAN_ROOM_ROOT}/cache/cargo"
export CARGO_TARGET_DIR="${CLEAN_ROOM_ROOT}/cache/cargo-target"
export TURBO_CACHE_DIR="${CLEAN_ROOM_ROOT}/cache/turbo"
export PLAYWRIGHT_BROWSERS_PATH="${CLEAN_ROOM_ROOT}/cache/playwright"
export XDG_CACHE_HOME="${CLEAN_ROOM_ROOT}/cache/xdg"
export npm_config_cache="${CLEAN_ROOM_ROOT}/cache/npm"
export SCCACHE_DIR="${CLEAN_ROOM_ROOT}/cache/sccache"
export CCACHE_DIR="${CLEAN_ROOM_ROOT}/cache/ccache"
export MISE_CACHE_DIR="${CLEAN_ROOM_ROOT}/cache/mise"
export MISE_DATA_DIR="${CLEAN_ROOM_ROOT}/data/mise"
export MISE_STATE_DIR="${CLEAN_ROOM_ROOT}/state/mise"
export BUILDX_CONFIG="${CLEAN_ROOM_ROOT}/config/buildx"
export TURBO_CACHE="local:w"

mkdir -p \
  "${GOCACHE}" \
  "${GOMODCACHE}" \
  "${GOLANGCI_LINT_CACHE}" \
  "${CARGO_HOME}" \
  "${CARGO_TARGET_DIR}" \
  "${TURBO_CACHE_DIR}" \
  "${PLAYWRIGHT_BROWSERS_PATH}" \
  "${XDG_CACHE_HOME}" \
  "${npm_config_cache}" \
  "${SCCACHE_DIR}" \
  "${CCACHE_DIR}" \
  "${MISE_CACHE_DIR}" \
  "${MISE_DATA_DIR}" \
  "${MISE_STATE_DIR}" \
  "${BUILDX_CONFIG}"

pnpm install \
  --frozen-lockfile \
  --force \
  --ignore-scripts \
  --store-dir "${CLEAN_ROOM_ROOT}/cache/pnpm-store"
pnpm --filter web exec playwright install chromium --with-deps

if [[ -z "${EARTHLY_AUDIO_MPV_PATH:-}" ]]; then
  mkdir -p "${CLEAN_ROOM_ROOT}/build/mpv"
  MPV_OUTPUT_DIRECTORY="${CLEAN_ROOM_ROOT}/tools/mpv" \
    MPV_BUILD_LOG="${CLEAN_ROOM_ROOT}/logs/mpv-build.log" \
    RUNNER_TEMP="${CLEAN_ROOM_ROOT}/build/mpv" \
    bash scripts/build-pinned-mpv.sh
  export EARTHLY_AUDIO_MPV_PATH="${CLEAN_ROOM_ROOT}/tools/mpv/mpv"
fi

mise run --force --task-cache=off ci:full
docker buildx create \
  --name "${CONTAINER_BUILDER_NAME}" \
  --driver docker-container
IS_CONTAINER_BUILDER_CREATED=true
docker buildx build \
  --builder "${CONTAINER_BUILDER_NAME}" \
  --pull \
  --no-cache \
  --output "type=oci,dest=${CLEAN_ROOM_ROOT}/navidrome-replacement.tar" \
  .

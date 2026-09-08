#!/usr/bin/env bash
set -euo pipefail
command="${1:?Desktop command is required}"
shift
if [[ "$command" == start ]]; then
  exec ./scripts/run-linux-webkit.sh "${CARGO_TARGET_DIR:-$PWD/src-tauri/target}/release/earthly-audio-desktop" "$@"
fi

configArgs=()
if [[ "${EARTHLY_STORAGE_RESOLVED_MODE:-}" == local ]]; then
  config="$(node -e 'const config = {build:{frontendDist:process.env.EARTHLY_WEB_DIST}}; if (process.argv[1] !== "build") config.bundle = {externalBin:[process.env.EARTHLY_MPV_DIRECTORY+"/mpv"]}; console.log(JSON.stringify(config))' "$command")"
  configArgs+=(--config "$config")
fi
case "$command" in
  build) exec tauri build --no-bundle "${configArgs[@]}" "$@" ;;
  sidecar|prepared)
    ./scripts/prepare-mpv-sidecar.sh
    if [[ "$command" == prepared ]]; then configArgs+=(--config '{"build":{"beforeBuildCommand":""}}'); fi
    exec tauri build --no-bundle --config src-tauri/tauri.sidecar.conf.json "${configArgs[@]}" "$@"
    ;;
  dev)
    ./scripts/prepare-mpv-sidecar.sh
    exec ./scripts/run-linux-webkit.sh tauri dev --config src-tauri/tauri.sidecar.conf.json "${configArgs[@]}" "$@"
    ;;
  *) echo "Unknown Desktop command: $command" >&2; exit 2 ;;
esac

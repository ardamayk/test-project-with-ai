#!/usr/bin/env bash
set -euo pipefail

STATE_DIRECTORY="${RUNNER_TEMP:?RUNNER_TEMP is required}/native-ci-dependencies"
MAX_WAIT_SECONDS=600

recordInstallationStatus() {
  local installExitCode="$?"
  printf '%s\n' "$installExitCode" > "$STATE_DIRECTORY/status.tmp"
  mv "$STATE_DIRECTORY/status.tmp" "$STATE_DIRECTORY/status"
}

installDependencies() {
  trap recordInstallationStatus EXIT
  sudo apt-get update
  sudo apt-get install -y --no-install-recommends \
    build-essential ffmpeg libayatana-appindicator3-dev \
    libass-dev libavcodec-dev libavfilter-dev libavformat-dev libavutil-dev \
    libplacebo-dev librsvg2-dev libssl-dev libswresample-dev libswscale-dev \
    libwebkit2gtk-4.1-dev libxdo-dev meson ninja-build pkg-config
}

startInstallation() {
  mkdir "$STATE_DIRECTORY"
  # Close inherited streams so this step can finish while cache actions run.
  nohup bash "$0" install > "$STATE_DIRECTORY/install.log" 2>&1 < /dev/null &
  printf '%s\n' "$!" > "$STATE_DIRECTORY/pid"
}

waitForInstallation() {
  local processId
  processId="$(cat "$STATE_DIRECTORY/pid")"
  for ((elapsed = 0; elapsed < MAX_WAIT_SECONDS; elapsed++)); do
    if [[ -f "$STATE_DIRECTORY/status" ]]; then
      cat "$STATE_DIRECTORY/install.log"
      return "$(cat "$STATE_DIRECTORY/status")"
    fi
    if ! kill -0 "$processId" 2>/dev/null && [[ ! -f "$STATE_DIRECTORY/status" ]]; then
      cat "$STATE_DIRECTORY/install.log"
      echo "Native dependency installation exited without a status." >&2
      return 1
    fi
    sleep 1
  done
  cat "$STATE_DIRECTORY/install.log"
  echo "Native dependency installation exceeded ${MAX_WAIT_SECONDS}s." >&2
  return 1
}

case "${1:-}" in
  start) startInstallation ;;
  install) installDependencies ;;
  wait) waitForInstallation ;;
  *) echo "Usage: prepare-native-ci-dependencies.sh start|install|wait" >&2; exit 2 ;;
esac

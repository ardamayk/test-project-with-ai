#!/usr/bin/env bash

set -euo pipefail

DISPLAY_MODE="${EARTHLY_AUDIO_DISPLAY_MODE:-auto}"

if [[ "$#" -eq 0 ]]; then
	echo "A Tauri/WebKitGTK command is required." >&2
	exit 2
fi

hasNvidiaGpu() {
	local vendorFile
	for vendorFile in /sys/class/drm/card*/device/vendor; do
		[[ -r "$vendorFile" ]] || continue
		[[ "$(<"$vendorFile")" == "0x10de" ]] && return 0
	done
	return 1
}

isWaylandSession() {
	[[ "${XDG_SESSION_TYPE:-}" == "wayland" || -n "${WAYLAND_DISPLAY:-}" ]]
}

resolveDisplayMode() {
	if [[ "$DISPLAY_MODE" != "auto" ]]; then
		printf '%s\n' "$DISPLAY_MODE"
		return
	fi
	if isWaylandSession && hasNvidiaGpu; then
		printf '%s\n' "wayland-nvidia"
	elif isWaylandSession; then
		printf '%s\n' "wayland"
	else
		printf '%s\n' "x11"
	fi
}

runWaylandNvidia() {
	exec env \
		-u WEBKIT_DMABUF_RENDERER_DISABLE_GBM \
		-u WEBKIT_DMABUF_RENDERER_FORCE_SHM \
		WEBKIT_DISABLE_DMABUF_RENDERER=0 \
		GDK_BACKEND=wayland \
		__NV_DISABLE_EXPLICIT_SYNC=1 \
		"$@"
}

runWayland() {
	exec env \
		-u WEBKIT_DMABUF_RENDERER_DISABLE_GBM \
		-u WEBKIT_DMABUF_RENDERER_FORCE_SHM \
		-u __NV_DISABLE_EXPLICIT_SYNC \
		WEBKIT_DISABLE_DMABUF_RENDERER=0 \
		GDK_BACKEND=wayland \
		"$@"
}

runWaylandShm() {
	exec env \
		-u WEBKIT_DMABUF_RENDERER_DISABLE_GBM \
		-u __NV_DISABLE_EXPLICIT_SYNC \
		WEBKIT_DISABLE_DMABUF_RENDERER=0 \
		GDK_BACKEND=wayland \
		WEBKIT_DMABUF_RENDERER_FORCE_SHM=1 \
		"$@"
}

runX11() {
	exec env \
		-u WEBKIT_DMABUF_RENDERER_DISABLE_GBM \
		-u WEBKIT_DMABUF_RENDERER_FORCE_SHM \
		-u __NV_DISABLE_EXPLICIT_SYNC \
		WEBKIT_DISABLE_DMABUF_RENDERER=0 \
		GDK_BACKEND=x11 \
		"$@"
}

case "$(resolveDisplayMode)" in
	wayland-nvidia) runWaylandNvidia "$@" ;;
	wayland) runWayland "$@" ;;
	wayland-shm) runWaylandShm "$@" ;;
	x11) runX11 "$@" ;;
	*)
		echo "Unsupported EARTHLY_AUDIO_DISPLAY_MODE: $DISPLAY_MODE" >&2
		echo "Expected: auto, wayland-nvidia, wayland, wayland-shm, or x11" >&2
		exit 2
		;;
esac

# Tauri, WebKitGTK, NVIDIA, and Wayland `Error 71`

Research date: 2026-08-27

## Conclusion

This is a known upstream graphics-stack problem, not an Earthly Audio or tray-indicator bug. Other Tauri and Wry developers have reproduced the exact `Gdk-Message: Error 71 (Protocol error)` failure, and the same failure occurs in WebKitGTK's own MiniBrowser. The strongest evidence points to an explicit-synchronization protocol violation around a GTK3/WebKitGTK EGL-backed Wayland surface on NVIDIA, rather than a general defect in Wayland or a KDE-only problem.

Tauri does not currently ship a stable fix. Its Tauri and Wry reports remain open and are labeled `status: upstream`. The most targeted native-Wayland workaround to test is NVIDIA's `__NV_DISABLE_EXPLICIT_SYNC=1`; shared-memory rendering and XWayland remain compatibility fallbacks.

## Local context

The affected system currently has:

- CachyOS, KDE Wayland, KWin 6.7.4
- NVIDIA GeForce RTX 4090, driver 610.57.04
- GTK 3.24.52, WebKitGTK 2.52.5
- `egl-wayland` 1.1.21 and `egl-wayland2` 1.0.1
- Tauri 2.11.5 and Wry 0.55.1

Native Wayland with normal hardware transport and with only `WEBKIT_DMABUF_RENDERER_DISABLE_GBM=1` both terminate with `Error 71`. Native Wayland with `WEBKIT_DMABUF_RENDERER_FORCE_SHM=1` runs but is slow.

## Direct upstream evidence

- [Tauri issue #10702](https://github.com/tauri-apps/tauri/issues/10702) tracks the exact error and remains open with `platform: Linux` and `status: upstream`. Its Wayland debug log exposes error 4 from `wp_linux_drm_syncobj_surface_v1`: `explicit sync is used, but no acquire point is set`.
- [Wry issue #1366](https://github.com/tauri-apps/wry/issues/1366) reproduces the same failure with Wry's simple example on Arch, NVIDIA 560.35.03, WebKitGTK 2.46.0, Plasma 6.1.5, and GNOME 47.0.1. It is also open and labeled `status: upstream`.
- [WebKitGTK bug 280210](https://bugs.webkit.org/show_bug.cgi?id=280210) reproduces the failure in WebKitGTK's own MiniBrowser, records the same missing-acquire-point protocol error, and confirms it under both Plasma and GNOME. It remains `NEW`, `Major`, and unassigned. This rules out Earthly Audio's Rust and web application code as the primary cause.
- The official Wayland protocol requires both acquire and release points whenever a non-null buffer is attached. Error value 4 is specifically `no_acquire_point`: [linux-drm-syncobj protocol, lines 126-203](https://gitlab.freedesktop.org/wayland/wayland-protocols/-/blob/main/staging/linux-drm-syncobj/linux-drm-syncobj-v1.xml#L126-203).
- An [NVIDIA egl-wayland collaborator's analysis](https://github.com/NVIDIA/egl-wayland/issues/179#issuecomment-3428324210) says the observed sequence is a protocol violation: after egl-wayland creates explicit-sync objects for an EGL-backed `wl_surface`, another layer attaches a buffer and commits it without the required synchronization points. The collaborator states that WebKitGTK should not attach the buffer in that situation.
- [GTK work item #8056](https://gitlab.gnome.org/GNOME/gtk/-/work_items/8056) reports the same Tauri/NVIDIA/Plasma failure, traces the attach to GTK3's `gdk_wayland_window_attach_image()`, and supplies a locally tested patch. This is reporter analysis and an unmerged proposal, not an accepted GTK fix.
- The older [WebKit bug 262607](https://bugs.webkit.org/show_bug.cgi?id=262607) documents NVIDIA proprietary-driver DMA-BUF failures predating this exact crash. It was resolved `WONTFIX`; a later comment reports the problem again on WebKitGTK 2.46.0.

The protocol violation is direct evidence. Assigning final ownership specifically to GTK3, WebKitGTK, or their interaction with egl-wayland is still an inference because no upstream project has accepted a definitive patch and closed the issue. The cross-compositor reproductions show that this is not specific to KWin. KWin is reporting the protocol error required when the client commits a buffer without an acquire point.

## What the environment variables do

These are implementation controls read by WebKitGTK or GTK, not Tauri configuration APIs. Their behavior is version-specific.

### `WEBKIT_DISABLE_DMABUF_RENDERER`

On the locally installed WebKitGTK 2.52.5 code path, any value other than the string `0` returns before either shared-memory or hardware transport is added. The resulting empty transport mode fails the accelerated-backing-store requirement. It is therefore the broadest fallback and explains the frequently reported performance loss; it does not literally select the shared-memory transport in this version. See [WebKitGTK 2.52.5 `AcceleratedBackingStore.cpp`, lines 82-145](https://github.com/WebKit/WebKit/blob/webkitgtk-2.52.5/Source/WebKit/UIProcess/gtk/AcceleratedBackingStore.cpp#L82-L145).

### `WEBKIT_DMABUF_RENDERER_FORCE_SHM`

WebKit first adds `SharedMemory`, then returns before adding `Hardware`. This keeps the renderer's shared-memory buffer transport while preventing hardware DMA-BUF transport. It is more precise than `WEBKIT_DISABLE_DMABUF_RENDERER`, but it loses the hardware-buffer/zero-copy path and can be substantially slower. See [the same WebKitGTK 2.52.5 transport selection](https://github.com/WebKit/WebKit/blob/webkitgtk-2.52.5/Source/WebKit/UIProcess/gtk/AcceleratedBackingStore.cpp#L82-L116).

### `WEBKIT_DMABUF_RENDERER_DISABLE_GBM`

This only prevents creation of `PlatformDisplayGBM` inside the Web process. WebKit then tries a surfaceless EGL display and finally the default display. It does not disable hardware buffer transport negotiation or the native-Wayland explicit-sync surface path. Its failure to prevent the local `Error 71` is therefore expected. See [WebKitGTK 2.52.5 `WebProcessGLib.cpp`, lines 148-190](https://github.com/WebKit/WebKit/blob/webkitgtk-2.52.5/Source/WebKit/WebProcess/glib/WebProcessGLib.cpp#L148-L190).

### `GDK_BACKEND`

GTK documents `GDK_BACKEND=wayland` as selecting the native Wayland display backend and `GDK_BACKEND=x11` as selecting X11. In a Wayland desktop session, the latter normally means XWayland. GTK can also accept an ordered comma-separated backend list. See the official [GTK 3 runtime environment documentation](https://docs.gtk.org/gtk3/running.html#environment-variables).

Changing to `x11` avoids this exact native-Wayland explicit-sync sequence, but it is an XWayland compatibility fallback, not a native-Wayland repair.

## Does Tauri have a fix?

No released fix was found as of the research date:

- Tauri uses Wry and the system WebKitGTK on Linux; this is documented by [Tauri's webview reference](https://v2.tauri.app/reference/webview-versions/) and [Wry's Linux platform notes](https://github.com/tauri-apps/wry#linux).
- The exact [Tauri](https://github.com/tauri-apps/tauri/issues/10702) and [Wry](https://github.com/tauri-apps/wry/issues/1366) issues remain open and marked upstream.
- A GTK4/WebKitGTK 6 direction exists, but [Wry PR #1530](https://github.com/tauri-apps/wry/pull/1530) is still a draft and [Tauri PR #14684](https://github.com/tauri-apps/tauri/pull/14684) remains open. It is future work, not a shipped solution or proof that this exact failure is fixed.

[Tao PR #979](https://github.com/tauri-apps/tao/pull/979) should not be treated as this bug's solution. It fixed a different Wayland protocol failure involving window maximize/resizable state; `Error 71` is a generic display protocol errno and can describe different violations.

## Practical recommendation

Test the NVIDIA-provided explicit-sync escape hatch first while keeping native Wayland and WebKit's hardware DMA-BUF transport enabled:

```bash
env \
  -u WEBKIT_DISABLE_DMABUF_RENDERER \
  -u WEBKIT_DMABUF_RENDERER_DISABLE_GBM \
  -u WEBKIT_DMABUF_RENDERER_FORCE_SHM \
  GDK_BACKEND=wayland \
  __NV_DISABLE_EXPLICIT_SYNC=1 \
  ./desktop/src-tauri/target/release/earthly-audio-desktop
```

NVIDIA added `__NV_DISABLE_EXPLICIT_SYNC` in [egl-wayland 1.1.15](https://github.com/NVIDIA/egl-wayland/releases/tag/1.1.15); value `1` disables use of `linux-drm-syncobj-v1`. NVIDIA's [egl-wayland2 explicit-sync compatibility notes](https://github.com/NVIDIA/egl-wayland2#explicit-sync-compatibility) recommend it for applications that incorrectly commit an EGL surface. This does not disable DMA-BUF hardware rendering, so it should usually perform better than forcing shared-memory transport. NVIDIA warns that operating without explicit sync can reduce performance or produce out-of-order frames, so it remains a workaround and must be tested for visual glitches.

Fallback order if that test is not reliable:

1. Native Wayland with `WEBKIT_DMABUF_RENDERER_FORCE_SHM=1`: verified to avoid the local crash, but slow.
2. `GDK_BACKEND=x11`: use XWayland for the currently working fast compatibility path, accepting loss of native-Wayland behavior.
3. Avoid using `WEBKIT_DMABUF_RENDERER_DISABLE_GBM=1` alone for this failure; both source and local testing show that it does not remove the failing explicit-sync path.

Do not set any workaround globally until it has been validated. Prefer applying it only to this application, and keep an opt-out so newer GTK/WebKit/NVIDIA packages can be retested without stale compatibility flags.

fn main() {
    configure_linux_webview_renderer();

    if let Err(error) = earthly_audio_desktop::run() {
        eprintln!("Earthly Audio Desktop failed: {error}");
        std::process::exit(1);
    }
}

#[cfg(target_os = "linux")]
fn configure_linux_webview_renderer() {
    const DISABLE_DMABUF_RENDERER: &str = "WEBKIT_DISABLE_DMABUF_RENDERER";
    if std::env::var_os(DISABLE_DMABUF_RENDERER).is_some() {
        return;
    }

    // WebKitGTK can otherwise create an unusable GBM surface on some Wayland systems.
    // SAFETY: main calls this before Tauri starts the webview or any application threads.
    unsafe {
        std::env::set_var(DISABLE_DMABUF_RENDERER, "1");
    }
}

#[cfg(not(target_os = "linux"))]
fn configure_linux_webview_renderer() {}

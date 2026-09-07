//! Always-on-top mini player: a second, undecorated webview showing the
//! compact player at the app's `/mini` route. It shares playback truth with
//! the main window through the `desktop-playback-state` broadcast, so no
//! extra playback plumbing is needed; only its position is remembered.

use serde::{Deserialize, Serialize};
use std::fs;
use std::path::PathBuf;
use tauri::{AppHandle, Manager, PhysicalPosition, WebviewUrl, WebviewWindowBuilder};

pub(crate) const MINI_WINDOW_LABEL: &str = "mini";
const MINI_WINDOW_WIDTH: f64 = 380.0;
const MINI_WINDOW_HEIGHT: f64 = 104.0;

#[derive(Clone, Copy, Debug, Default, Deserialize, PartialEq, Serialize)]
pub struct MiniWindowPlacement {
    pub x: i32,
    pub y: i32,
}

/// Remembers where the mini player was last placed, next to the other
/// desktop settings files.
pub struct MiniWindowStore {
    path: PathBuf,
}

impl MiniWindowStore {
    pub fn new(path: PathBuf) -> Self {
        Self { path }
    }

    pub fn load(&self) -> Option<MiniWindowPlacement> {
        let raw = fs::read_to_string(&self.path).ok()?;
        serde_json::from_str(&raw).ok()
    }

    pub fn save(&self, placement: MiniWindowPlacement) -> Result<(), String> {
        if let Some(parent) = self.path.parent() {
            fs::create_dir_all(parent)
                .map_err(|error| format!("Failed to create settings directory: {error}"))?;
        }
        let raw = serde_json::to_string(&placement)
            .map_err(|error| format!("Failed to encode mini player placement: {error}"))?;
        fs::write(&self.path, raw)
            .map_err(|error| format!("Failed to persist mini player placement: {error}"))
    }
}

pub(crate) fn is_open(app: &AppHandle) -> bool {
    app.get_webview_window(MINI_WINDOW_LABEL).is_some()
}

pub(crate) fn open(app: &AppHandle, store: &MiniWindowStore) -> Result<(), String> {
    if let Some(window) = app.get_webview_window(MINI_WINDOW_LABEL) {
        return window
            .show()
            .and_then(|_| window.set_focus())
            .map_err(|error| error.to_string());
    }
    let mut builder =
        WebviewWindowBuilder::new(app, MINI_WINDOW_LABEL, WebviewUrl::App("mini".into()))
            .title("Earthly Audio")
            .inner_size(MINI_WINDOW_WIDTH, MINI_WINDOW_HEIGHT)
            .resizable(false)
            .decorations(false)
            .always_on_top(true)
            .skip_taskbar(true);
    if let Some(placement) = store.load() {
        builder = builder.position(f64::from(placement.x), f64::from(placement.y));
    }
    builder
        .build()
        .map(|_| ())
        .map_err(|error| format!("Mini player window could not be opened: {error}"))
}

pub(crate) fn close(app: &AppHandle, store: &MiniWindowStore) -> Result<(), String> {
    let Some(window) = app.get_webview_window(MINI_WINDOW_LABEL) else {
        return Ok(());
    };
    if let Ok(PhysicalPosition { x, y }) = window.outer_position()
        && let Err(error) = store.save(MiniWindowPlacement { x, y })
    {
        eprintln!("Mini player placement was not saved: {error}");
    }
    window
        .close()
        .map_err(|error| format!("Mini player window could not be closed: {error}"))
}

pub(crate) fn toggle(app: &AppHandle, store: &MiniWindowStore) -> Result<(), String> {
    if is_open(app) {
        close(app, store)
    } else {
        open(app, store)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn placement_round_trips_through_the_store() {
        let mut random = [0_u8; 8];
        getrandom::fill(&mut random).expect("random temporary name");
        let path = std::env::temp_dir().join(format!(
            "earthly-audio-mini-{}/mini-window.json",
            u64::from_le_bytes(random)
        ));
        let store = MiniWindowStore::new(path.clone());
        assert_eq!(store.load(), None);

        store
            .save(MiniWindowPlacement { x: 120, y: -40 })
            .expect("save placement");
        assert_eq!(store.load(), Some(MiniWindowPlacement { x: 120, y: -40 }));

        fs::write(&path, "not json").expect("corrupt file");
        assert_eq!(store.load(), None, "a corrupt file reads as no placement");
        let _ = fs::remove_dir_all(path.parent().expect("parent"));
    }
}

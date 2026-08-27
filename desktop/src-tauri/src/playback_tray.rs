use serde_json::Value;
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::{TrayIcon, TrayIconBuilder};
use tauri::{AppHandle, Wry};

pub(crate) const TRAY_NEXT_ID: &str = "playback-next";
pub(crate) const TRAY_OPEN_ID: &str = "playback-open";
pub(crate) const TRAY_PREVIOUS_ID: &str = "playback-previous";
pub(crate) const TRAY_QUIT_ID: &str = "playback-quit";
pub(crate) const TRAY_TOGGLE_ID: &str = "playback-toggle";

#[derive(Clone)]
pub(crate) struct PlaybackTray {
    source: MenuItem<Wry>,
    previous: MenuItem<Wry>,
    toggle: MenuItem<Wry>,
    next: MenuItem<Wry>,
    _icon: TrayIcon<Wry>,
}

impl PlaybackTray {
    pub(crate) fn start(
        app: &AppHandle,
        on_menu_event: impl Fn(&AppHandle, &str) + Send + Sync + 'static,
    ) -> tauri::Result<Self> {
        let view = PlaybackTrayView::from_playback(None, false);
        let source = menu_item(app, "playback-source", &view.source_label, false)?;
        let previous = menu_item(app, TRAY_PREVIOUS_ID, view.previous_label, false)?;
        let toggle = menu_item(app, TRAY_TOGGLE_ID, view.toggle_label, false)?;
        let next = menu_item(app, TRAY_NEXT_ID, view.next_label, false)?;
        let open = menu_item(app, TRAY_OPEN_ID, view.open_label, true)?;
        let quit = menu_item(app, TRAY_QUIT_ID, view.quit_label, true)?;
        let separator = PredefinedMenuItem::separator(app)?;
        let menu = Menu::with_items(
            app,
            &[&source, &previous, &toggle, &next, &separator, &open, &quit],
        )?;
        let mut builder = TrayIconBuilder::with_id("earthly-audio-playback").menu(&menu);
        if let Some(icon) = app.default_window_icon() {
            builder = builder.icon(icon.clone());
        }
        let icon = builder
            .on_menu_event(move |app, event| on_menu_event(app, event.id().as_ref()))
            .build(app)?;
        Ok(Self {
            source,
            previous,
            toggle,
            next,
            _icon: icon,
        })
    }

    pub(crate) fn update(&self, source: Option<&Value>, is_playing: bool) -> tauri::Result<()> {
        let view = PlaybackTrayView::from_playback(source, is_playing);
        let has_source = source.is_some();
        self.source.set_text(view.source_label)?;
        self.previous.set_enabled(has_source)?;
        self.toggle.set_enabled(has_source)?;
        self.toggle.set_text(view.toggle_label)?;
        self.next.set_enabled(has_source)
    }
}

fn menu_item(
    app: &AppHandle,
    id: &str,
    label: &str,
    is_enabled: bool,
) -> tauri::Result<MenuItem<Wry>> {
    MenuItem::with_id(app, id, label, is_enabled, None::<&str>)
}

#[derive(Clone, Debug, PartialEq)]
pub(crate) struct PlaybackTrayView {
    pub(crate) source_label: String,
    pub(crate) previous_label: &'static str,
    pub(crate) toggle_label: &'static str,
    pub(crate) next_label: &'static str,
    pub(crate) open_label: &'static str,
    pub(crate) quit_label: &'static str,
}

impl PlaybackTrayView {
    pub(crate) fn from_playback(source: Option<&Value>, is_playing: bool) -> Self {
        Self {
            source_label: source
                .and_then(source_label)
                .unwrap_or("Nothing playing")
                .to_owned(),
            previous_label: "Previous",
            toggle_label: if is_playing { "Pause" } else { "Play" },
            next_label: "Next",
            open_label: "Open Earthly Audio",
            quit_label: "Quit Earthly Audio",
        }
    }
}

fn source_label(source: &Value) -> Option<&str> {
    match source.get("type").and_then(Value::as_str) {
        Some("track") => source.pointer("/track/title").and_then(Value::as_str),
        Some("radio-station") => source.pointer("/station/name").and_then(Value::as_str),
        Some("catalog-preview") => source.pointer("/result/name").and_then(Value::as_str),
        _ => None,
    }
}

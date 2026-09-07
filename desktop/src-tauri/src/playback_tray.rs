use serde_json::Value;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::{TrayIcon, TrayIconBuilder};
use tauri::{AppHandle, Wry};

/// Tooltip updates carry the playhead, which changes several times a second;
/// D-Bus tray hosts are only told this often.
const TOOLTIP_REFRESH_INTERVAL: Duration = Duration::from_secs(5);

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
    icon: TrayIcon<Wry>,
    tooltip: Arc<Mutex<TooltipThrottle>>,
}

#[derive(Debug, Default)]
pub(crate) struct TooltipThrottle {
    last_text: Option<String>,
    last_sent_at: Option<Instant>,
}

impl TooltipThrottle {
    /// Whether `text` should reach the tray now: always on a change of the
    /// source or playing state (the part before the separator), otherwise at
    /// most once per interval.
    pub(crate) fn should_send(&mut self, text: &str, now: Instant) -> bool {
        let heading_changed = self
            .last_text
            .as_deref()
            .map(tooltip_heading)
            .is_none_or(|previous| previous != tooltip_heading(text));
        let is_due = self
            .last_sent_at
            .is_none_or(|sent| now.duration_since(sent) >= TOOLTIP_REFRESH_INTERVAL);
        if !heading_changed && (!is_due || self.last_text.as_deref() == Some(text)) {
            return false;
        }
        self.last_text = Some(text.to_owned());
        self.last_sent_at = Some(now);
        true
    }
}

fn tooltip_heading(text: &str) -> &str {
    text.split(" · ").next().unwrap_or(text)
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
            icon,
            tooltip: Arc::new(Mutex::new(TooltipThrottle::default())),
        })
    }

    pub(crate) fn update(
        &self,
        source: Option<&Value>,
        is_playing: bool,
        position: PlaybackPosition,
    ) -> tauri::Result<()> {
        let view = PlaybackTrayView::from_playback(source, is_playing);
        let has_source = source.is_some();
        self.source.set_text(&view.source_label)?;
        self.previous.set_enabled(has_source)?;
        self.toggle.set_enabled(has_source)?;
        self.toggle.set_text(view.toggle_label)?;
        self.next.set_enabled(has_source)?;
        let tooltip = view.tooltip(position);
        let should_send = self
            .tooltip
            .lock()
            .map(|mut throttle| throttle.should_send(&tooltip, Instant::now()))
            .unwrap_or(true);
        if should_send {
            self.icon.set_tooltip(Some(tooltip.as_str()))?;
        }
        Ok(())
    }
}

/// Playhead and length in seconds, for the tooltip.
#[derive(Clone, Copy, Debug, Default, PartialEq)]
pub(crate) struct PlaybackPosition {
    pub(crate) current_seconds: f64,
    pub(crate) duration_seconds: f64,
}

pub(crate) fn format_clock(seconds: f64) -> String {
    if !seconds.is_finite() || seconds < 0.0 {
        return "0:00".to_owned();
    }
    let total = seconds.floor() as u64;
    format!("{}:{:02}", total / 60, total % 60)
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
    pub(crate) artist_label: Option<String>,
    pub(crate) is_playing: bool,
    pub(crate) has_source: bool,
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
            artist_label: source.and_then(artist_label).map(str::to_owned),
            is_playing,
            has_source: source.is_some(),
            previous_label: "Previous",
            toggle_label: if is_playing { "Pause" } else { "Play" },
            next_label: "Next",
            open_label: "Open Earthly Audio",
            quit_label: "Quit Earthly Audio",
        }
    }

    /// "Title – Artist · 1:23 / 4:56", "Paused: …", or the app name.
    pub(crate) fn tooltip(&self, position: PlaybackPosition) -> String {
        if !self.has_source {
            return "Earthly Audio".to_owned();
        }
        let mut heading = self.source_label.clone();
        if let Some(artist) = self.artist_label.as_deref() {
            heading.push_str(" – ");
            heading.push_str(artist);
        }
        if !self.is_playing {
            heading = format!("Paused: {heading}");
        }
        if position.duration_seconds > 0.0 {
            format!(
                "{heading} · {} / {}",
                format_clock(position.current_seconds),
                format_clock(position.duration_seconds)
            )
        } else {
            heading
        }
    }

    /// Main window title: the track while something plays, else the app name.
    pub(crate) fn window_title(&self) -> String {
        if !self.has_source {
            return "Earthly Audio".to_owned();
        }
        match self.artist_label.as_deref() {
            Some(artist) => format!("{} – {artist} · Earthly Audio", self.source_label),
            None => format!("{} · Earthly Audio", self.source_label),
        }
    }
}

fn artist_label(source: &Value) -> Option<&str> {
    match source.get("type").and_then(Value::as_str) {
        Some("track") => source.pointer("/track/artistName").and_then(Value::as_str),
        _ => None,
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

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn track() -> Value {
        json!({
            "type": "track",
            "track": { "id": "track-1", "title": "Nemo", "artistName": "Nightwish" }
        })
    }

    #[test]
    fn tooltip_names_the_track_state_and_position() {
        let playing = PlaybackTrayView::from_playback(Some(&track()), true);
        assert_eq!(
            playing.tooltip(PlaybackPosition {
                current_seconds: 754.0,
                duration_seconds: 2700.0
            }),
            "Nemo – Nightwish · 12:34 / 45:00"
        );
        let paused = PlaybackTrayView::from_playback(Some(&track()), false);
        assert_eq!(
            paused.tooltip(PlaybackPosition::default()),
            "Paused: Nemo – Nightwish"
        );
        assert_eq!(
            PlaybackTrayView::from_playback(None, false).tooltip(PlaybackPosition::default()),
            "Earthly Audio"
        );
    }

    #[test]
    fn window_title_follows_the_source() {
        assert_eq!(
            PlaybackTrayView::from_playback(Some(&track()), true).window_title(),
            "Nemo – Nightwish · Earthly Audio"
        );
        assert_eq!(
            PlaybackTrayView::from_playback(None, false).window_title(),
            "Earthly Audio"
        );
    }

    #[test]
    fn tooltip_throttle_sends_changes_immediately_but_progress_only_periodically() {
        let mut throttle = TooltipThrottle::default();
        let start = Instant::now();
        assert!(throttle.should_send("Nemo – Nightwish · 0:01 / 4:36", start));
        assert!(!throttle.should_send(
            "Nemo – Nightwish · 0:02 / 4:36",
            start + Duration::from_secs(1)
        ));
        assert!(throttle.should_send(
            "Paused: Nemo – Nightwish · 0:02 / 4:36",
            start + Duration::from_secs(1)
        ));
        assert!(throttle.should_send(
            "Paused: Nemo – Nightwish · 0:09 / 4:36",
            start + Duration::from_secs(7)
        ));
        assert!(!throttle.should_send(
            "Paused: Nemo – Nightwish · 0:09 / 4:36",
            start + Duration::from_secs(20)
        ));
    }
}

use crate::playback::{PlaybackCommandError, PlaybackController};
use crate::playback_lifecycle::{
    PlaybackLifecycle, PlaybackSessionSnapshot, PlaybackSnapshotStore,
};
use std::sync::{Arc, Mutex};

#[derive(Clone, Copy, Debug, PartialEq)]
pub(crate) enum DesktopPlaybackAction {
    OpenMainWindow,
    CloseMainWindow,
    TogglePlay,
    Previous,
    Next,
    Quit,
}

pub(crate) trait DesktopPlaybackShell {
    fn show_main_window(&self) -> Result<(), String>;
    fn hide_main_window(&self) -> Result<(), String>;
    fn exit(&self);
}

pub(crate) fn dispatch_desktop_playback_action(
    action: DesktopPlaybackAction,
    playback: &PlaybackController,
    lifecycle: &Arc<Mutex<PlaybackLifecycle>>,
    snapshot_store: &PlaybackSnapshotStore,
    shell: &dyn DesktopPlaybackShell,
) -> Result<(), PlaybackCommandError> {
    match action {
        DesktopPlaybackAction::OpenMainWindow => {
            shell.show_main_window().map_err(PlaybackCommandError::new)
        }
        DesktopPlaybackAction::CloseMainWindow => close_main_window(lifecycle, shell),
        DesktopPlaybackAction::TogglePlay => playback.toggle_play().map(|_| ()),
        DesktopPlaybackAction::Previous => playback.previous().map(|_| ()),
        DesktopPlaybackAction::Next => playback.next().map(|_| ()),
        DesktopPlaybackAction::Quit => quit(playback, lifecycle, snapshot_store, shell),
    }
}

fn close_main_window(
    lifecycle: &Arc<Mutex<PlaybackLifecycle>>,
    shell: &dyn DesktopPlaybackShell,
) -> Result<(), PlaybackCommandError> {
    shell
        .hide_main_window()
        .map_err(PlaybackCommandError::new)?;
    lifecycle
        .lock()
        .map_err(|_| PlaybackCommandError::new("Playback lifecycle state is unavailable."))?
        .close_main_window();
    Ok(())
}

fn quit(
    playback: &PlaybackController,
    lifecycle: &Arc<Mutex<PlaybackLifecycle>>,
    snapshot_store: &PlaybackSnapshotStore,
    shell: &dyn DesktopPlaybackShell,
) -> Result<(), PlaybackCommandError> {
    let playback_state = playback.state()?;
    let snapshot = PlaybackSessionSnapshot::from_serializable_state(&playback_state)
        .map_err(|error| PlaybackCommandError::new(error.to_string()))?;
    lifecycle
        .lock()
        .map_err(|_| PlaybackCommandError::new("Playback lifecycle state is unavailable."))?
        .explicit_quit(snapshot_store, &snapshot)
        .map_err(|error| PlaybackCommandError::new(error.to_string()))?;
    playback.shutdown();
    shell.exit();
    Ok(())
}

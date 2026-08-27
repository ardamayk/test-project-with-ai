# Rust-owned desktop playback session

The Desktop Client's Rust layer owns its Playback Session and mpv lifecycle; the React renderer sends commands and observes events through a PlaybackEngine interface. The Web Client supplies a browser-audio adapter to the same interface. This keeps the shared UI independent from its audio engine and lets Desktop Background Playback survive window closure or renderer reload.

One mpv child remains idle between Playback Sources. After one unexpected crash, Rust restarts it once and restores the current source and playhead; a second failure stops recovery and surfaces an error. Explicit application relaunch restores the last Playback Session Snapshot paused, never autoplaying. If a Direct ALSA Output Device disappears, playback pauses and asks for another device or System Output instead of silently changing routes.

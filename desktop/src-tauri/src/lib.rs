pub mod adaptive_system_rate;
mod connection;
mod media_proxy;
pub mod output_device;
mod playback;
mod playback_app_actions;
mod playback_lifecycle;
mod playback_navigation;
#[cfg(test)]
mod playback_test_support;
mod playback_tray;
pub mod processing;
mod queue_events;
pub mod telemetry;

use adaptive_system_rate::{AdaptiveSystemRateController, CommandPipeWireRateAdapter};
use connection::{
    ConnectionCheck, ConnectionError, ConnectionErrorCode, ConnectionStore, HttpBridge,
    HttpRequest, HttpResponse, ServerOrigin,
};
use media_proxy::MediaProxy;
use playback::{PlaybackCommandError, PlaybackController, PlaybackSessionState, PlaybackStatus};
use playback_app_actions::{
    DesktopPlaybackAction, DesktopPlaybackShell, dispatch_desktop_playback_action,
};
use playback_lifecycle::{PlaybackLifecycle, PlaybackSnapshotStore};
use playback_tray::{
    PlaybackTray, TRAY_NEXT_ID, TRAY_OPEN_ID, TRAY_PREVIOUS_ID, TRAY_QUIT_ID, TRAY_TOGGLE_ID,
};
use processing::{
    EqualizerPreset, FileProcessingSettingsStorage, OutputMode, ProcessingController,
    ProcessingProfile, ReplayGainMode, mpv_configuration_for,
};
use queue_events::QueueEventService;
use serde::Serialize;
use serde_json::Value;
use std::collections::BTreeMap;
use std::sync::{Arc, Mutex, RwLock};
use tauri::{Emitter, Manager, State, WindowEvent};
use telemetry::CommandPipeWireObserver;

const CONNECTION_FILE_NAME: &str = "server-connection.json";
const PLAYBACK_SNAPSHOT_FILE_NAME: &str = "playback-session.json";
const PROCESSING_SETTINGS_FILE_NAME: &str = "processing-settings.json";
const PLAYBACK_STATE_EVENT: &str = "desktop-playback-state";
const CONNECTION_CHANGED_EVENT: &str = "server-connection-changed";
const QUEUE_EVENTS_ERROR_EVENT: &str = "desktop-queue-events-error";
const QUEUE_INVALIDATED_EVENT: &str = "desktop-queue-invalidated";
const COVER_PROTOCOL: &str = "earthly-media";
const COVER_REQUEST_HEADERS: &[&str] = &["accept", "authorization", "range"];
const COVER_RESPONSE_HEADERS: &[&str] = &[
    "accept-ranges",
    "cache-control",
    "content-length",
    "content-range",
    "content-type",
    "etag",
    "last-modified",
];

struct AppState {
    playback: PlaybackController,
    processing: Mutex<ProcessingController>,
    playback_lifecycle: Arc<Mutex<PlaybackLifecycle>>,
    playback_snapshot_store: PlaybackSnapshotStore,
    _playback_tray: PlaybackTray,
    bridge: Arc<HttpBridge>,
    store: ConnectionStore,
    origin: Arc<RwLock<Option<ServerOrigin>>>,
    media_proxy: MediaProxy,
    queue_events: QueueEventService,
}

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct ServerConnection {
    origin: String,
}

#[tauri::command]
fn get_server_connection(
    state: State<'_, AppState>,
) -> Result<Option<ServerConnection>, ConnectionError> {
    let origin = state.origin.read().map_err(|_| state_error())?;
    Ok(origin.as_ref().map(|origin| ServerConnection {
        origin: origin.as_str().to_owned(),
    }))
}

#[tauri::command]
async fn test_server_connection(
    state: State<'_, AppState>,
    origin: String,
) -> Result<ConnectionCheck, ConnectionError> {
    let origin = ServerOrigin::parse(&origin)?;
    state.bridge.test_server(&origin).await
}

#[tauri::command]
async fn save_server_connection(
    app: tauri::AppHandle,
    state: State<'_, AppState>,
    origin: String,
) -> Result<ConnectionCheck, ConnectionError> {
    let origin = ServerOrigin::parse(&origin)?;
    let check = state.bridge.test_server(&origin).await?;
    state.store.save(&origin)?;
    *state.origin.write().map_err(|_| state_error())? = Some(origin);
    state.queue_events.reconnect();
    app.emit(CONNECTION_CHANGED_EVENT, &check)
        .map_err(|_| state_error())?;
    Ok(check)
}

#[tauri::command]
async fn desktop_http_request(
    state: State<'_, AppState>,
    request: HttpRequest,
) -> Result<HttpResponse, ConnectionError> {
    let origin = state
        .origin
        .read()
        .map_err(|_| state_error())?
        .clone()
        .ok_or_else(|| {
            ConnectionError::new(
                ConnectionErrorCode::InvalidOrigin,
                "Configure a Music Server before sending desktop HTTP requests.",
            )
        })?;
    state.bridge.send(&origin, request).await
}

#[tauri::command]
fn get_media_proxy_url(state: State<'_, AppState>) -> String {
    state.media_proxy.base_url().to_owned()
}

#[tauri::command]
fn desktop_reconnect_queue_events(state: State<'_, AppState>) {
    state.queue_events.reconnect();
}

#[tauri::command]
fn get_desktop_playback_state(
    state: State<'_, AppState>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    state.playback.state()
}

#[tauri::command]
fn desktop_playback_renderer_ready(
    app: tauri::AppHandle,
    state: State<'_, AppState>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    state
        .playback_lifecycle
        .lock()
        .map_err(|_| PlaybackCommandError::new("Playback lifecycle state is unavailable."))?
        .renderer_attached();
    let playback_state = state.playback.state()?;
    app.emit(PLAYBACK_STATE_EVENT, &playback_state)
        .map_err(|_| PlaybackCommandError::new("Playback state event could not be published."))?;
    Ok(playback_state)
}

#[tauri::command]
fn desktop_playback_quit(
    app: tauri::AppHandle,
    state: State<'_, AppState>,
) -> Result<(), PlaybackCommandError> {
    dispatch_application_action(&app, &state, DesktopPlaybackAction::Quit)
}

#[tauri::command]
fn desktop_playback_play(
    state: State<'_, AppState>,
    source: Option<Value>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    if let Some(source) = source.as_ref() {
        validate_playback_source(source, state.media_proxy.base_url())?;
    }
    state.playback.play(source)
}

#[tauri::command]
fn desktop_playback_pause(
    state: State<'_, AppState>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    state.playback.pause()
}

#[tauri::command]
fn desktop_playback_stop(
    state: State<'_, AppState>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    state.playback.stop()
}

#[tauri::command]
fn desktop_playback_toggle_play(
    state: State<'_, AppState>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    state.playback.toggle_play()
}

#[tauri::command]
fn desktop_playback_sync_queue_context(
    state: State<'_, AppState>,
    sources: Vec<Value>,
    current_index: Option<usize>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    for source in &sources {
        validate_playback_source(source, state.media_proxy.base_url())?;
    }
    state.playback.sync_queue_context(sources, current_index)
}

#[tauri::command]
fn desktop_playback_previous(
    state: State<'_, AppState>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    state.playback.previous()
}

#[tauri::command]
fn desktop_playback_next(
    state: State<'_, AppState>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    state.playback.next()
}

#[tauri::command]
fn desktop_playback_seek(
    state: State<'_, AppState>,
    seconds: f64,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    state.playback.seek(seconds)
}

#[tauri::command]
fn desktop_playback_set_volume(
    state: State<'_, AppState>,
    value: f64,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    update_processing(&state, |processing| processing.set_software_volume(value))
}

#[tauri::command]
fn desktop_playback_set_processing_profile(
    state: State<'_, AppState>,
    profile: ProcessingProfile,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    update_processing(&state, |processing| processing.set_profile(profile))
}

#[tauri::command]
fn desktop_playback_set_replay_gain(
    state: State<'_, AppState>,
    mode: ReplayGainMode,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    update_processing(&state, |processing| {
        if mode == ReplayGainMode::Off {
            processing.disable_replay_gain()
        } else {
            processing.enable_replay_gain(mode)
        }
    })
}

#[tauri::command]
fn desktop_playback_set_equalizer_preset(
    state: State<'_, AppState>,
    preset: EqualizerPreset,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    update_processing(&state, |processing| {
        processing.apply_equalizer_preset(preset)
    })
}

#[tauri::command]
fn desktop_playback_set_equalizer_gain(
    state: State<'_, AppState>,
    index: usize,
    value: f64,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    update_processing(&state, |processing| {
        processing.set_equalizer_gain(index, value)
    })
}

#[tauri::command]
fn desktop_playback_refresh_output_devices(
    state: State<'_, AppState>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    state.playback.refresh_output_devices()
}

#[tauri::command]
fn desktop_playback_select_direct_alsa_output(
    state: State<'_, AppState>,
    device_id: String,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    let mut processing = state
        .processing
        .lock()
        .map_err(|_| PlaybackCommandError::new("Output Mode settings are unavailable."))?;
    let playback_state = state.playback.select_direct_alsa_output(&device_id)?;
    if playback_state.output_mode == OutputMode::DirectAlsa {
        if let Err(error) = processing.select_direct_alsa_output(&device_id) {
            if let Err(rollback_error) = state.playback.fallback_to_system_output() {
                eprintln!(
                    "Direct ALSA Output rollback failed after persistence error: {}",
                    rollback_error.message
                );
            }
            return Err(PlaybackCommandError::new(error));
        }
    }
    Ok(playback_state)
}

#[tauri::command]
fn desktop_playback_fallback_to_system_output(
    state: State<'_, AppState>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    let mut processing = state
        .processing
        .lock()
        .map_err(|_| PlaybackCommandError::new("Output Mode settings are unavailable."))?;
    let previous_mode = processing.output_mode();
    let previous_device_id = processing.selected_output_device_id().map(str::to_owned);
    let playback_state = state.playback.fallback_to_system_output()?;
    if let Err(error) = processing.set_output_mode(OutputMode::System) {
        restore_native_output_mode(
            &state.playback,
            previous_mode,
            previous_device_id.as_deref(),
        );
        return Err(PlaybackCommandError::new(error));
    }
    Ok(playback_state)
}

#[tauri::command]
fn desktop_playback_enable_adaptive_system_rate(
    state: State<'_, AppState>,
    is_confirmed: bool,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    let mut processing = state
        .processing
        .lock()
        .map_err(|_| PlaybackCommandError::new("Output Mode settings are unavailable."))?;
    let playback_state = state.playback.enable_adaptive_system_rate(is_confirmed)?;
    if let Err(error) = processing.set_output_mode(OutputMode::AdaptiveSystemRate) {
        if let Err(rollback_error) = state.playback.fallback_to_system_output() {
            eprintln!(
                "Adaptive System Rate rollback failed after persistence error: {}",
                rollback_error.message
            );
        }
        return Err(PlaybackCommandError::new(error));
    }
    Ok(playback_state)
}

fn restore_native_output_mode(
    playback: &PlaybackController,
    output_mode: OutputMode,
    device_id: Option<&str>,
) {
    let result = match (output_mode, device_id) {
        (OutputMode::DirectAlsa, Some(device_id)) => {
            playback.select_direct_alsa_output(device_id).map(|_| ())
        }
        (OutputMode::AdaptiveSystemRate, _) => {
            playback.enable_adaptive_system_rate(true).map(|_| ())
        }
        _ => playback.fallback_to_system_output().map(|_| ()),
    };
    if let Err(error) = result {
        eprintln!("Output Mode rollback failed: {}", error.message);
    }
}

fn update_processing(
    state: &AppState,
    change: impl FnOnce(&mut ProcessingController) -> Result<(), String>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    let mut processing = state
        .processing
        .lock()
        .map_err(|_| PlaybackCommandError::new("Processing Profile state is unavailable."))?;
    let previous_state = processing.state().clone();
    change(&mut processing).map_err(PlaybackCommandError::new)?;
    let processing_state = processing.state().clone();
    let configuration = processing.mpv_configuration();
    match state
        .playback
        .apply_processing(processing_state, &configuration)
    {
        Ok(playback_state) => {
            if let Err(error) = processing.restore(playback_state.processing.clone()) {
                rollback_processing(&state.playback, &mut processing, previous_state);
                return Err(PlaybackCommandError::new(error));
            }
            Ok(playback_state)
        }
        Err(error) => {
            rollback_processing(&state.playback, &mut processing, previous_state);
            Err(error)
        }
    }
}

fn rollback_processing(
    playback: &PlaybackController,
    processing: &mut ProcessingController,
    previous_state: processing::ProcessingState,
) {
    if let Err(error) = processing.restore(previous_state.clone()) {
        eprintln!("Processing Profile rollback persistence failed: {error}");
    }
    let configuration = mpv_configuration_for(&previous_state);
    if let Err(error) = playback.apply_processing(previous_state, &configuration) {
        eprintln!(
            "Native mpv Processing Profile rollback failed: {}",
            error.message
        );
    }
}

#[tauri::command]
fn desktop_playback_toggle_shuffle(
    state: State<'_, AppState>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    state.playback.toggle_shuffle()
}

#[tauri::command]
fn desktop_playback_cycle_repeat_mode(
    state: State<'_, AppState>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    state.playback.cycle_repeat_mode()
}

fn validate_playback_source(
    source: &Value,
    media_proxy_base_url: &str,
) -> Result<(), PlaybackCommandError> {
    let playback_url = source
        .get("playbackUrl")
        .and_then(Value::as_str)
        .ok_or_else(|| PlaybackCommandError::new("Playback Source URL is missing."))?;
    let required_prefix = format!("{media_proxy_base_url}/");
    if !playback_url.starts_with(&required_prefix) {
        return Err(PlaybackCommandError::new(
            "Native playback is restricted to the private Desktop media proxy.",
        ));
    }
    Ok(())
}

fn state_error() -> ConnectionError {
    ConnectionError::new(
        ConnectionErrorCode::Storage,
        "Desktop connection state is unavailable. Restart the Desktop Client.",
    )
}

fn handle_tray_menu_event(app: &tauri::AppHandle, id: &str) {
    let action = match id {
        TRAY_OPEN_ID => DesktopPlaybackAction::OpenMainWindow,
        TRAY_TOGGLE_ID => DesktopPlaybackAction::TogglePlay,
        TRAY_PREVIOUS_ID => DesktopPlaybackAction::Previous,
        TRAY_NEXT_ID => DesktopPlaybackAction::Next,
        TRAY_QUIT_ID => DesktopPlaybackAction::Quit,
        _ => return,
    };
    let state = app.state::<AppState>();
    let result = dispatch_application_action(app, &state, action).map_err(|error| error.message);
    if let Err(error) = result {
        eprintln!("Desktop playback tray action '{id}' failed: {error}");
    }
}

fn dispatch_application_action(
    app: &tauri::AppHandle,
    state: &AppState,
    action: DesktopPlaybackAction,
) -> Result<(), PlaybackCommandError> {
    dispatch_desktop_playback_action(
        action,
        &state.playback,
        &state.playback_lifecycle,
        &state.playback_snapshot_store,
        &TauriDesktopPlaybackShell { app },
    )
}

fn handle_main_window_close(window: &tauri::Window, event: &WindowEvent) {
    if window.label() != "main" {
        return;
    }
    let WindowEvent::CloseRequested { api, .. } = event else {
        return;
    };
    api.prevent_close();
    let state = window.state::<AppState>();
    if let Err(error) = dispatch_application_action(
        window.app_handle(),
        &state,
        DesktopPlaybackAction::CloseMainWindow,
    ) {
        eprintln!("Desktop main window close action failed: {}", error.message);
    }
}

struct TauriDesktopPlaybackShell<'a> {
    app: &'a tauri::AppHandle,
}

impl DesktopPlaybackShell for TauriDesktopPlaybackShell<'_> {
    fn show_main_window(&self) -> Result<(), String> {
        let window = self
            .app
            .get_webview_window("main")
            .ok_or_else(|| "Desktop main window is unavailable.".to_owned())?;
        window
            .show()
            .and_then(|_| window.set_focus())
            .map_err(|error| error.to_string())
    }

    fn hide_main_window(&self) -> Result<(), String> {
        self.app
            .get_webview_window("main")
            .ok_or_else(|| "Desktop main window is unavailable.".to_owned())?
            .hide()
            .map_err(|error| error.to_string())
    }

    fn exit(&self) {
        self.app.exit(0);
    }
}

fn cover_http_request(
    request: &tauri::http::Request<Vec<u8>>,
) -> Result<HttpRequest, ConnectionError> {
    let path = request.uri().path();
    let segments = path.split('/').collect::<Vec<_>>();
    let is_cover_path = matches!(
        segments.as_slice(),
        ["", "api", "v1", "library", "albums", album_id, "cover"] if !album_id.is_empty()
    );
    if !is_cover_path || !matches!(request.method().as_str(), "GET" | "HEAD") {
        return Err(ConnectionError::new(
            ConnectionErrorCode::InvalidRequest,
            "Desktop cover protocol only serves album cover GET and HEAD requests.",
        ));
    }
    let headers = request
        .headers()
        .iter()
        .filter(|(name, _)| COVER_REQUEST_HEADERS.contains(&name.as_str()))
        .filter_map(|(name, value)| {
            value
                .to_str()
                .ok()
                .map(|value| (name.as_str().to_owned(), value.to_owned()))
        })
        .collect::<BTreeMap<_, _>>();
    let url = match request.uri().query() {
        Some(query) => format!("{path}?{query}"),
        None => path.to_owned(),
    };
    Ok(HttpRequest {
        method: request.method().as_str().to_owned(),
        url,
        headers,
        body: None,
    })
}

async fn cover_protocol_response(
    bridge: Arc<HttpBridge>,
    origin: Arc<RwLock<Option<ServerOrigin>>>,
    request: tauri::http::Request<Vec<u8>>,
) -> tauri::http::Response<Vec<u8>> {
    let result = async {
        let request = cover_http_request(&request)?;
        let origin = origin
            .read()
            .map_err(|_| state_error())?
            .clone()
            .ok_or_else(|| {
                ConnectionError::new(
                    ConnectionErrorCode::InvalidOrigin,
                    "Configure a Music Server before requesting desktop covers.",
                )
            })?;
        bridge.send(&origin, request).await
    }
    .await;

    match result {
        Ok(response) => {
            let mut builder = tauri::http::Response::builder().status(response.status);
            for (name, value) in response.headers {
                if COVER_RESPONSE_HEADERS.contains(&name.as_str()) {
                    builder = builder.header(name, value);
                }
            }
            builder.body(response.body).unwrap_or_default()
        }
        Err(error) => tauri::http::Response::builder()
            .status(protocol_error_status(error.code))
            .header("content-type", "text/plain; charset=utf-8")
            .body(error.message.into_bytes())
            .unwrap_or_default(),
    }
}

fn protocol_error_status(code: ConnectionErrorCode) -> u16 {
    match code {
        ConnectionErrorCode::InvalidOrigin | ConnectionErrorCode::InvalidRequest => 400,
        ConnectionErrorCode::Unreachable => 502,
        ConnectionErrorCode::ProxyUnavailable => 503,
        ConnectionErrorCode::InvalidResponse
        | ConnectionErrorCode::ResponseTooLarge
        | ConnectionErrorCode::CapabilityMismatch
        | ConnectionErrorCode::Storage => 500,
    }
}

pub fn run() -> tauri::Result<()> {
    tauri::Builder::default()
        .on_window_event(handle_main_window_close)
        .register_asynchronous_uri_scheme_protocol(COVER_PROTOCOL, |context, request, responder| {
            let state = context.app_handle().state::<AppState>();
            let bridge = state.bridge.clone();
            let origin = state.origin.clone();
            tauri::async_runtime::spawn(async move {
                responder.respond(cover_protocol_response(bridge, origin, request).await);
            });
        })
        .setup(|app| {
            let config_directory = app.path().app_config_dir()?;
            let store = ConnectionStore::new(config_directory.join(CONNECTION_FILE_NAME));
            let playback_snapshot_store =
                PlaybackSnapshotStore::new(config_directory.join(PLAYBACK_SNAPSHOT_FILE_NAME));
            let saved_playback = playback_snapshot_store
                .load()
                .map_err(std::io::Error::other)?;
            let origin = Arc::new(RwLock::new(store.load()?));
            let bridge = Arc::new(HttpBridge::new()?);
            let media_proxy = MediaProxy::start(bridge.clone(), origin.clone())?;
            let saved_playback = saved_playback
                .map(|snapshot| snapshot.rebind_media_proxy(media_proxy.base_url()))
                .transpose()
                .map_err(std::io::Error::other)?;
            let playback_lifecycle = Arc::new(Mutex::new(PlaybackLifecycle::new()));
            let mut processing =
                ProcessingController::open(Box::new(FileProcessingSettingsStorage::new(
                    config_directory.join(PROCESSING_SETTINGS_FILE_NAME),
                )))
                .map_err(std::io::Error::other)?;
            if processing.output_mode() == OutputMode::AdaptiveSystemRate {
                processing
                    .set_output_mode(OutputMode::System)
                    .map_err(std::io::Error::other)?;
            }
            let playback_tray = PlaybackTray::start(app.handle(), handle_tray_menu_event)?;
            let playback_event_tray = playback_tray.clone();
            let app_handle = app.handle().clone();
            let adaptive_rate_adapter = Arc::new(CommandPipeWireRateAdapter::new());
            let adaptive_system_rate =
                AdaptiveSystemRateController::recover_startup(adaptive_rate_adapter.clone())
                    .unwrap_or_else(|error| {
                        eprintln!("Adaptive System Rate startup recovery failed: {error}");
                        AdaptiveSystemRateController::new(adaptive_rate_adapter)
                    });
            let playback = PlaybackController::start_default_with_lifecycle(
                playback_lifecycle.clone(),
                Arc::new(CommandPipeWireObserver::new()),
                adaptive_system_rate,
                move |state| {
                    if let Err(error) = playback_event_tray.update(
                        state.source.as_ref(),
                        state.status == PlaybackStatus::Playing,
                    ) {
                        eprintln!("Desktop playback tray update failed: {error}");
                    }
                    if let Err(error) = app_handle.emit(PLAYBACK_STATE_EVENT, state) {
                        eprintln!("Desktop playback state event failed: {error}");
                    }
                },
            )
            .map_err(std::io::Error::other)?;
            playback.refresh_output_devices().unwrap_or_else(|error| {
                eprintln!(
                    "Raw ALSA hardware startup enumeration failed: {}",
                    error.message
                );
                playback.state().unwrap_or_default()
            });
            if processing.output_mode() == OutputMode::DirectAlsa
                && let Some(device_id) = processing.selected_output_device_id()
                && let Err(error) = playback.select_direct_alsa_output(device_id)
            {
                eprintln!(
                    "Saved Direct ALSA Output could not be restored: {}",
                    error.message
                );
            }
            if let Some(snapshot) = saved_playback.as_ref() {
                playback
                    .restore_paused(snapshot)
                    .map_err(|error| std::io::Error::other(error.message))?;
            }
            playback
                .apply_processing(processing.state().clone(), &processing.mpv_configuration())
                .map_err(|error| std::io::Error::other(error.message))?;
            let queue_event_app = app.handle().clone();
            let queue_error_app = app.handle().clone();
            let queue_events = QueueEventService::start(
                bridge.clone(),
                origin.clone(),
                move |event| {
                    if let Err(error) = queue_event_app.emit(QUEUE_INVALIDATED_EVENT, event) {
                        eprintln!("Desktop Queue event emission failed: {error}");
                    }
                },
                move |message| {
                    if let Err(error) = queue_error_app.emit(QUEUE_EVENTS_ERROR_EVENT, message) {
                        eprintln!("Desktop Queue error emission failed: {error}");
                    }
                },
            );
            app.manage(AppState {
                playback,
                processing: Mutex::new(processing),
                playback_lifecycle,
                playback_snapshot_store,
                _playback_tray: playback_tray,
                bridge,
                store,
                origin,
                media_proxy,
                queue_events,
            });
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            get_server_connection,
            test_server_connection,
            save_server_connection,
            desktop_http_request,
            get_media_proxy_url,
            desktop_reconnect_queue_events,
            get_desktop_playback_state,
            desktop_playback_renderer_ready,
            desktop_playback_quit,
            desktop_playback_play,
            desktop_playback_pause,
            desktop_playback_stop,
            desktop_playback_toggle_play,
            desktop_playback_sync_queue_context,
            desktop_playback_previous,
            desktop_playback_next,
            desktop_playback_seek,
            desktop_playback_set_volume,
            desktop_playback_set_processing_profile,
            desktop_playback_set_replay_gain,
            desktop_playback_set_equalizer_preset,
            desktop_playback_set_equalizer_gain,
            desktop_playback_refresh_output_devices,
            desktop_playback_select_direct_alsa_output,
            desktop_playback_fallback_to_system_output,
            desktop_playback_enable_adaptive_system_rate,
            desktop_playback_toggle_shuffle,
            desktop_playback_cycle_repeat_mode
        ])
        .run(tauri::generate_context!())
}

#[cfg(test)]
mod tests {
    use super::{cover_http_request, validate_playback_source};
    use serde_json::json;

    #[test]
    fn cover_protocol_discards_custom_authority_and_preserves_path() {
        let request = tauri::http::Request::builder()
            .uri("earthly-media://attacker.example/api/v1/library/albums/album-1/cover?size=large")
            .body(Vec::new())
            .expect("cover request");

        let forwarded = cover_http_request(&request).expect("allowed cover request");

        assert_eq!(
            forwarded.url,
            "/api/v1/library/albums/album-1/cover?size=large"
        );
    }

    #[test]
    fn cover_protocol_rejects_track_streams() {
        let request = tauri::http::Request::builder()
            .uri("earthly-media://localhost/api/v1/tracks/track-1/stream")
            .body(Vec::new())
            .expect("track request");

        assert!(cover_http_request(&request).is_err());
    }

    #[test]
    fn native_playback_rejects_urls_outside_private_media_proxy() {
        let source = json!({
            "type": "track",
            "playbackUrl": "https://attacker.example/track.flac"
        });

        assert!(validate_playback_source(&source, "http://127.0.0.1:43129/private-token").is_err());
    }
}

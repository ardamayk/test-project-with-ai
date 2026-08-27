mod connection;
mod media_proxy;
mod playback;
mod queue_events;

use connection::{
    ConnectionCheck, ConnectionError, ConnectionErrorCode, ConnectionStore, HttpBridge,
    HttpRequest, HttpResponse, ServerOrigin,
};
use media_proxy::MediaProxy;
use playback::{PlaybackCommandError, PlaybackController, PlaybackSessionState};
use queue_events::QueueEventService;
use serde::Serialize;
use serde_json::Value;
use std::collections::BTreeMap;
use std::sync::{Arc, RwLock};
use tauri::{Emitter, Manager, State};

const CONNECTION_FILE_NAME: &str = "server-connection.json";
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
    state.playback.set_volume(value)
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
        .register_asynchronous_uri_scheme_protocol(COVER_PROTOCOL, |context, request, responder| {
            let state = context.app_handle().state::<AppState>();
            let bridge = state.bridge.clone();
            let origin = state.origin.clone();
            tauri::async_runtime::spawn(async move {
                responder.respond(cover_protocol_response(bridge, origin, request).await);
            });
        })
        .setup(|app| {
            let store =
                ConnectionStore::new(app.path().app_config_dir()?.join(CONNECTION_FILE_NAME));
            let origin = Arc::new(RwLock::new(store.load()?));
            let bridge = Arc::new(HttpBridge::new()?);
            let media_proxy = MediaProxy::start(bridge.clone(), origin.clone())?;
            let app_handle = app.handle().clone();
            let playback = PlaybackController::start_default(move |state| {
                if let Err(error) = app_handle.emit("desktop-playback-state", state) {
                    eprintln!("Desktop playback state event failed: {error}");
                }
            })
            .map_err(std::io::Error::other)?;
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
            desktop_playback_play,
            desktop_playback_pause,
            desktop_playback_stop,
            desktop_playback_toggle_play,
            desktop_playback_seek,
            desktop_playback_set_volume,
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

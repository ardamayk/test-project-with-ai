use crate::connection::{ConnectionError, HttpBridge, HttpResponse, ServerOrigin};
use futures_util::StreamExt;
use serde::Serialize;
use std::collections::HashMap;
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use tauri::ipc::Channel;
use tokio_util::io::ReaderStream;
use tokio_util::sync::CancellationToken;
use uuid::Uuid;

pub const SUPPORTED_EXTENSIONS: &[&str] = &["flac", "mp3", "m4a", "ogg", "opus", "wav"];

#[derive(Clone, Debug, Eq, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ImportSelection {
    pub id: String,
    pub name: String,
    pub size: u64,
}

#[derive(Default)]
pub struct ImportSelectionStore {
    paths: Mutex<HashMap<String, PathBuf>>,
    cancellations: Mutex<HashMap<String, CancellationToken>>,
}

impl ImportSelectionStore {
    pub fn register_paths(
        &self,
        roots: impl IntoIterator<Item = PathBuf>,
    ) -> Result<Vec<ImportSelection>, DesktopImportError> {
        let mut candidates = Vec::new();
        for root in roots {
            collect_audio_files(&root, false, &mut candidates)?;
        }
        candidates.sort();
        let mut paths = self.paths.lock().map_err(|_| state_error())?;
        let mut selections = Vec::with_capacity(candidates.len());
        for path in candidates {
            let metadata = fs::metadata(&path).map_err(|_| selection_error())?;
            let Some(name) = path
                .file_name()
                .and_then(|value| value.to_str())
                .map(str::to_owned)
            else {
                eprintln!("Desktop import skipped a selected path with a non-Unicode filename");
                continue;
            };
            let id = Uuid::new_v4().to_string();
            paths.insert(id.clone(), path);
            selections.push(ImportSelection {
                id,
                name,
                size: metadata.len(),
            });
        }
        Ok(selections)
    }

    pub fn path(&self, id: &str) -> Result<Option<PathBuf>, DesktopImportError> {
        Ok(self
            .paths
            .lock()
            .map_err(|_| state_error())?
            .get(id)
            .cloned())
    }

    pub fn begin_upload(&self, upload_id: &str) -> Result<CancellationToken, DesktopImportError> {
        let mut cancellations = self.cancellations.lock().map_err(|_| state_error())?;
        Ok(cancellations
            .entry(upload_id.to_owned())
            .or_insert_with(CancellationToken::new)
            .clone())
    }

    pub fn cancel(&self, upload_id: &str) -> Result<(), DesktopImportError> {
        self.cancellations
            .lock()
            .map_err(|_| state_error())?
            .entry(upload_id.to_owned())
            .or_insert_with(CancellationToken::new)
            .cancel();
        Ok(())
    }

    pub fn finish_upload(&self, upload_id: &str) -> Result<(), DesktopImportError> {
        self.cancellations
            .lock()
            .map_err(|_| state_error())?
            .remove(upload_id);
        Ok(())
    }
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ImportUploadProgress {
    pub sent_bytes: u64,
    pub total_bytes: u64,
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DesktopImportError {
    pub code: &'static str,
    pub message: String,
}

impl From<ConnectionError> for DesktopImportError {
    fn from(error: ConnectionError) -> Self {
        Self {
            code: "transport_error",
            message: error.message,
        }
    }
}

pub async fn upload_selection(
    store: &ImportSelectionStore,
    bridge: &HttpBridge,
    origin: &ServerOrigin,
    selection_id: &str,
    upload_id: &str,
    job_id: &str,
    on_progress: Channel<ImportUploadProgress>,
) -> Result<HttpResponse, DesktopImportError> {
    let cancellation = store.begin_upload(upload_id)?;
    let result = upload_selection_inner(
        store,
        bridge,
        origin,
        selection_id,
        job_id,
        on_progress,
        cancellation.clone(),
    )
    .await;
    store.finish_upload(upload_id)?;
    if cancellation.is_cancelled() {
        return Err(canceled_error());
    }
    result
}

async fn upload_selection_inner(
    store: &ImportSelectionStore,
    bridge: &HttpBridge,
    origin: &ServerOrigin,
    selection_id: &str,
    job_id: &str,
    on_progress: Channel<ImportUploadProgress>,
    cancellation: CancellationToken,
) -> Result<HttpResponse, DesktopImportError> {
    let (filename, body) = prepare_upload(store, selection_id, on_progress).await?;
    let upload_path = import_upload_path(job_id)?;
    tokio::select! {
        _ = cancellation.cancelled() => Err(canceled_error()),
        result = bridge.send_import(
            origin,
            &upload_path,
            &filename,
            content_type(&filename),
            body,
        ) => result.map_err(Into::into),
    }
}

async fn prepare_upload(
    store: &ImportSelectionStore,
    selection_id: &str,
    on_progress: Channel<ImportUploadProgress>,
) -> Result<(String, reqwest::Body), DesktopImportError> {
    let path = store
        .path(selection_id)?
        .ok_or_else(invalid_selection_error)?;
    let filename = path
        .file_name()
        .and_then(|value| value.to_str())
        .ok_or_else(selection_error)?
        .to_owned();
    let file = tokio::fs::File::open(&path)
        .await
        .map_err(|_| selection_error())?;
    let total_bytes = file.metadata().await.map_err(|_| selection_error())?.len();
    Ok((filename, progress_body(file, total_bytes, on_progress)))
}

fn progress_body(
    file: tokio::fs::File,
    total_bytes: u64,
    on_progress: Channel<ImportUploadProgress>,
) -> reqwest::Body {
    let sent_bytes = Arc::new(AtomicU64::new(0));
    let progress_bytes = sent_bytes.clone();
    let stream = ReaderStream::new(file).map(move |chunk| {
        let chunk = chunk?;
        let sent =
            progress_bytes.fetch_add(chunk.len() as u64, Ordering::Relaxed) + chunk.len() as u64;
        if let Err(error) = on_progress.send(ImportUploadProgress {
            sent_bytes: sent,
            total_bytes,
        }) {
            eprintln!("Desktop import progress receiver closed: {error}");
        }
        Ok::<_, std::io::Error>(chunk)
    });
    reqwest::Body::wrap_stream(stream)
}

pub fn import_upload_path(job_id: &str) -> Result<String, DesktopImportError> {
    if job_id.is_empty()
        || !job_id
            .bytes()
            .all(|value| value.is_ascii_alphanumeric() || value == b'-' || value == b'_')
    {
        return Err(DesktopImportError {
            code: "invalid_request",
            message: "Managed Import job ID is invalid.".to_owned(),
        });
    }
    Ok(format!("/api/v1/imports/{job_id}/file"))
}

fn collect_audio_files(
    path: &Path,
    has_hidden_parent: bool,
    files: &mut Vec<PathBuf>,
) -> Result<(), DesktopImportError> {
    let metadata = fs::symlink_metadata(path).map_err(|_| selection_error())?;
    if metadata.file_type().is_symlink() {
        return Ok(());
    }
    let is_hidden = has_hidden_parent
        || path
            .file_name()
            .and_then(|value| value.to_str())
            .is_some_and(|value| value.starts_with('.'));
    if is_hidden {
        return Ok(());
    }
    if metadata.is_dir() {
        for entry in fs::read_dir(path).map_err(|_| selection_error())? {
            collect_audio_files(&entry.map_err(|_| selection_error())?.path(), false, files)?;
        }
    } else if is_supported_audio(path) {
        files.push(path.to_owned());
    }
    Ok(())
}

fn is_supported_audio(path: &Path) -> bool {
    path.extension()
        .and_then(|value| value.to_str())
        .is_some_and(|value| SUPPORTED_EXTENSIONS.contains(&value.to_ascii_lowercase().as_str()))
}

fn content_type(filename: &str) -> &'static str {
    match Path::new(filename)
        .extension()
        .and_then(|value| value.to_str())
        .map(str::to_ascii_lowercase)
        .as_deref()
    {
        Some("flac") => "audio/flac",
        Some("mp3") => "audio/mpeg",
        Some("m4a") => "audio/mp4",
        Some("ogg") | Some("opus") => "audio/ogg",
        Some("wav") => "audio/wav",
        _ => "application/octet-stream",
    }
}

fn selection_error() -> DesktopImportError {
    DesktopImportError {
        code: "selection_unavailable",
        message: "Selected import file could not be read.".to_owned(),
    }
}

fn invalid_selection_error() -> DesktopImportError {
    DesktopImportError {
        code: "invalid_selection",
        message: "Selected import file is no longer available. Select it again.".to_owned(),
    }
}

fn state_error() -> DesktopImportError {
    DesktopImportError {
        code: "state_unavailable",
        message: "Desktop import state is unavailable.".to_owned(),
    }
}

fn canceled_error() -> DesktopImportError {
    DesktopImportError {
        code: "canceled",
        message: "Managed Import upload canceled.".to_owned(),
    }
}

#[cfg(test)]
mod tests {
    use super::{
        ImportSelectionStore, ImportUploadProgress, content_type, import_upload_path,
        upload_selection,
    };
    use crate::connection::{HttpBridge, ServerOrigin};
    use std::fs;
    use std::io::Read;
    use std::net::TcpListener;
    use std::path::PathBuf;
    use std::sync::Arc;
    use std::sync::mpsc;
    use std::thread;
    use std::time::Duration;
    use std::time::{SystemTime, UNIX_EPOCH};
    use tauri::ipc::Channel;

    #[test]
    fn recursive_selection_returns_only_supported_visible_audio() {
        let root = std::env::temp_dir().join(format!(
            "earthly-import-{}",
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .expect("system time")
                .as_nanos()
        ));
        fs::create_dir_all(root.join("disc/.hidden")).expect("create fixture");
        for name in [
            "album.flac",
            "song.mp3",
            "recording.m4a",
            "track.ogg",
            "voice.opus",
            "archive.wav",
        ] {
            fs::write(root.join("disc").join(name), b"audio").expect("write audio");
        }
        fs::write(root.join("disc/cover.jpg"), b"image").expect("write sidecar");
        fs::write(root.join("disc/.hidden/secret.mp3"), b"audio").expect("write hidden");

        let store = ImportSelectionStore::default();
        let selected = store
            .register_paths([root.clone()])
            .expect("register folder");

        assert_eq!(selected.len(), 6);
        assert_eq!(
            selected
                .iter()
                .map(|selection| selection.name.as_str())
                .collect::<Vec<_>>(),
            [
                "album.flac",
                "archive.wav",
                "recording.m4a",
                "song.mp3",
                "track.ogg",
                "voice.opus"
            ]
        );
        assert!(selected.iter().all(|selection| selection.size == 5));
        assert!(
            selected
                .iter()
                .all(|selection| !selection.id.contains("track"))
        );

        fs::remove_dir_all(root).expect("remove fixture");
    }

    #[test]
    fn every_supported_format_uses_an_audio_content_type() {
        for filename in [
            "track.flac",
            "track.mp3",
            "track.m4a",
            "track.ogg",
            "track.opus",
            "track.wav",
        ] {
            assert!(content_type(filename).starts_with("audio/"), "{filename}");
        }
    }

    #[test]
    fn active_upload_can_be_canceled_without_removing_selection() {
        let store = ImportSelectionStore::default();
        let cancellation = store.begin_upload("upload-1").expect("begin upload");

        store.cancel("upload-1").expect("cancel upload");

        assert!(cancellation.is_cancelled());
    }

    #[test]
    fn cancellation_arriving_before_upload_registration_is_retained() {
        let store = ImportSelectionStore::default();

        store.cancel("upload-1").expect("cancel upload");
        let cancellation = store.begin_upload("upload-1").expect("begin upload");

        assert!(cancellation.is_cancelled());
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn cancellation_interrupts_wait_for_server_response() {
        let fixture = temporary_fixture_path("flac");
        fs::write(&fixture, b"audio bytes").expect("write fixture");
        let store = Arc::new(ImportSelectionStore::default());
        let selection = store
            .register_paths([fixture.clone()])
            .expect("register file")
            .remove(0);
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind server");
        let origin = ServerOrigin::parse(&format!(
            "http://{}",
            listener.local_addr().expect("server address")
        ))
        .expect("parse origin");
        let (request_sender, request_receiver) = mpsc::channel();
        let (release_sender, release_receiver) = mpsc::channel();
        thread::spawn(move || {
            let (mut stream, _) = listener.accept().expect("accept request");
            let mut request = [0_u8; 4096];
            let read = stream.read(&mut request).expect("read request");
            assert!(read > 0, "request must contain headers");
            request_sender.send(()).expect("request received");
            release_receiver.recv().expect("release server");
        });
        let upload_store = store.clone();
        let upload = tokio::spawn(async move {
            upload_selection(
                &upload_store,
                &HttpBridge::new().expect("create bridge"),
                &origin,
                &selection.id,
                "upload-1",
                "job-1",
                Channel::<ImportUploadProgress>::new(|_| Ok(())),
            )
            .await
        });
        request_receiver.recv().expect("wait for request");

        store.cancel("upload-1").expect("cancel upload");
        let error = tokio::time::timeout(Duration::from_millis(500), upload)
            .await
            .expect("upload stops promptly")
            .expect("upload task")
            .expect_err("upload is canceled");

        assert_eq!(error.code, "canceled");
        release_sender.send(()).expect("release server");
        fs::remove_file(fixture).expect("remove fixture");
    }

    #[test]
    fn upload_path_rejects_values_that_can_escape_import_endpoint() {
        assert_eq!(
            import_upload_path("01990b44-f903-7b30-a9b1-b851396cdd45").expect("valid job id"),
            "/api/v1/imports/01990b44-f903-7b30-a9b1-b851396cdd45/file"
        );
        assert!(import_upload_path("../health").is_err());
        assert!(import_upload_path("job?redirect=http://attacker.example").is_err());
    }

    fn temporary_fixture_path(extension: &str) -> PathBuf {
        std::env::temp_dir().join(format!(
            "earthly-import-{}.{}",
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .expect("system time")
                .as_nanos(),
            extension
        ))
    }
}

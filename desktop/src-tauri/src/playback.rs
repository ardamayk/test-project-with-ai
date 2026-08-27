use crate::playback_lifecycle::{
    PlaybackLifecycle, PlaybackLifecycleAction, PlaybackSessionSnapshot,
};
use crate::playback_navigation::{PlaybackQueueContext, PlaybackSource, QueueDirection};
use crate::processing::{
    AppliedMpvProcessingConfiguration, EffectiveReplayGainMode, MpvProcessingConfiguration,
    OutputMode, ProcessingState, ReplayGainMode,
};
use crate::telemetry::{
    ObservedMpvProperties, PipeWireObservation, PipeWireObserver, PlaybackTelemetry,
    ProcessingObservation, SourceObservation,
};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::fs;
use std::io::{BufRead, BufReader, ErrorKind, Write};
use std::os::unix::fs::PermissionsExt;
use std::os::unix::net::UnixStream;
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{self, Receiver, RecvTimeoutError, Sender};
use std::sync::{Arc, Mutex};
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant};

const DEFAULT_VOLUME: f64 = 0.8;
const EVENT_POLL_INTERVAL: Duration = Duration::from_millis(50);
const MPV_COMMAND_TIMEOUT: Duration = Duration::from_secs(5);
const MPV_PINNED_VERSION: &str = include_str!("../mpv-version.txt");
const MPV_START_TIMEOUT: Duration = Duration::from_secs(3);
const MPV_SHUTDOWN_TIMEOUT: Duration = Duration::from_secs(1);
const PLAYBACK_FAILED_MESSAGE: &str = "Playback failed. Check the source and try again.";
#[cfg(not(test))]
const PATH_REFRESH_INTERVAL: Duration = Duration::from_secs(2);
#[cfg(test)]
const PATH_REFRESH_INTERVAL: Duration = Duration::from_millis(20);

#[derive(Clone, Debug, Deserialize, PartialEq, Serialize)]
#[serde(rename_all = "lowercase")]
pub(crate) enum PlaybackStatus {
    Idle,
    Playing,
    Paused,
    Ended,
    Error,
}

#[derive(Clone, Debug, Deserialize, PartialEq, Serialize)]
#[serde(rename_all = "lowercase")]
enum RepeatMode {
    Off,
    Once,
    Loop,
}

#[derive(Clone, Debug, Deserialize, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct PlaybackStateError {
    code: String,
    message: String,
}

#[derive(Clone, Debug, Deserialize, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct PlaybackSessionState {
    pub(crate) source: Option<Value>,
    pub(crate) output_mode: OutputMode,
    pub(crate) status: PlaybackStatus,
    pub(crate) current_time: f64,
    pub(crate) duration: f64,
    volume: f64,
    shuffle_enabled: bool,
    repeat_mode: RepeatMode,
    error: Option<PlaybackStateError>,
    pub(crate) processing: ProcessingState,
    telemetry: PlaybackTelemetry,
}

impl Default for PlaybackSessionState {
    fn default() -> Self {
        Self {
            source: None,
            output_mode: OutputMode::System,
            status: PlaybackStatus::Idle,
            current_time: 0.0,
            duration: 0.0,
            volume: DEFAULT_VOLUME,
            shuffle_enabled: false,
            repeat_mode: RepeatMode::Off,
            error: None,
            processing: ProcessingState::default(),
            telemetry: PlaybackTelemetry::native_system_output(
                SourceObservation::unknown(),
                ObservedMpvProperties::unknown(),
                None,
                ProcessingObservation::unknown(),
            ),
        }
    }
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct PlaybackCommandError {
    code: &'static str,
    pub(crate) message: String,
}

impl PlaybackCommandError {
    pub(crate) fn new(message: impl Into<String>) -> Self {
        Self {
            code: "playback-failed",
            message: message.into(),
        }
    }
}

pub(crate) enum MpvEvent {
    Time(f64),
    Duration(f64),
    Paused(bool),
    Decoder(ObservedMpvProperties),
    Ended,
    Error(String),
    ExitedUnexpectedly(String),
}

pub(crate) trait MpvProcessAdapter: Send + Sync {
    fn load(&self, url: &str) -> Result<(), String>;
    fn set_paused(&self, is_paused: bool) -> Result<(), String>;
    fn seek(&self, seconds: f64) -> Result<(), String>;
    fn set_volume(&self, value: f64) -> Result<(), String>;
    fn apply_processing(
        &self,
        configuration: &MpvProcessingConfiguration,
    ) -> Result<AppliedMpvProcessingConfiguration, String> {
        Ok(AppliedMpvProcessingConfiguration {
            volume_percent: configuration.volume_percent,
            replay_gain_mode: configuration.replay_gain_mode,
            audio_filters: configuration.audio_filters.clone(),
        })
    }
    fn stop(&self) -> Result<(), String>;
    fn shutdown(&self);
}

enum MpvWorkerRequest {
    Command {
        command: Value,
        response: Sender<Result<Value, String>>,
    },
    Shutdown,
}

pub(crate) struct RealMpvProcess {
    requests: Sender<MpvWorkerRequest>,
    worker: Mutex<Option<JoinHandle<()>>>,
    ipc_directory: PathBuf,
}

impl RealMpvProcess {
    pub(crate) fn start_default() -> Result<(Box<dyn MpvProcessAdapter>, Receiver<MpvEvent>), String>
    {
        let binary = resolve_mpv_binary();
        let (process, events, _) = Self::start(binary, Vec::new())?;
        Ok((process, events))
    }

    fn start(
        binary: PathBuf,
        extra_arguments: Vec<String>,
    ) -> Result<(Box<dyn MpvProcessAdapter>, Receiver<MpvEvent>, PathBuf), String> {
        ensure_pinned_mpv(&binary)?;
        let ipc_directory = create_private_ipc_directory()?;
        match start_mpv_worker(&binary, &ipc_directory, extra_arguments) {
            Ok((requests, worker, events)) => {
                let process = Self {
                    requests,
                    worker: Mutex::new(Some(worker)),
                    ipc_directory: ipc_directory.clone(),
                };
                process.observe_properties()?;
                Ok((Box::new(process), events, ipc_directory))
            }
            Err(error) => {
                remove_ipc_directory(&ipc_directory, "after mpv startup failed");
                Err(error)
            }
        }
    }

    fn observe_properties(&self) -> Result<(), String> {
        self.command(json!(["observe_property", 1, "time-pos"]))?;
        self.command(json!(["observe_property", 2, "duration"]))?;
        self.command(json!(["observe_property", 3, "pause"]))?;
        self.command(json!(["observe_property", 4, "audio-params"]))
            .map(|_| ())
    }

    fn command(&self, command: Value) -> Result<Value, String> {
        let (response_sender, response_receiver) = mpsc::channel();
        self.requests
            .send(MpvWorkerRequest::Command {
                command,
                response: response_sender,
            })
            .map_err(|_| "Native mpv process is not running.".to_owned())?;
        response_receiver
            .recv_timeout(MPV_COMMAND_TIMEOUT)
            .map_err(|_| "Native mpv command timed out.".to_owned())?
    }

    fn stop_worker(&self) {
        let worker = match self.worker.lock() {
            Ok(mut worker) => worker.take(),
            Err(_) => {
                eprintln!("Pinned mpv worker shutdown failed: worker state is unavailable.");
                None
            }
        };
        let Some(worker) = worker else {
            remove_ipc_directory(&self.ipc_directory, "after repeated mpv shutdown");
            return;
        };
        if self.requests.send(MpvWorkerRequest::Shutdown).is_err() {
            eprintln!("Pinned mpv worker shutdown request failed: worker channel is closed.");
        }
        if worker.join().is_err() {
            eprintln!("Pinned mpv worker shutdown failed: worker thread panicked.");
        }
        remove_ipc_directory(&self.ipc_directory, "during mpv shutdown");
    }
}

fn remove_ipc_directory(path: &PathBuf, context: &str) {
    if let Err(error) = fs::remove_dir_all(path)
        && error.kind() != ErrorKind::NotFound
    {
        eprintln!(
            "Private mpv IPC cleanup failed {context} at '{}': {error}",
            path.display()
        );
    }
}

fn resolve_mpv_binary() -> PathBuf {
    if let Some(binary) = std::env::var_os("EARTHLY_AUDIO_MPV_PATH") {
        return PathBuf::from(binary);
    }
    match std::env::current_exe() {
        Ok(executable) => {
            if let Some(directory) = executable.parent() {
                let packaged_binary = directory.join("mpv");
                if packaged_binary.is_file() {
                    return packaged_binary;
                }
            } else {
                eprintln!("Packaged mpv lookup failed: current executable has no parent path.");
            }
        }
        Err(error) => {
            eprintln!("Packaged mpv lookup failed: current executable is unknown: {error}")
        }
    }
    let development_binary = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("binaries/mpv");
    if development_binary.is_file() {
        return development_binary;
    }
    PathBuf::from("/usr/bin/mpv")
}

impl MpvProcessAdapter for RealMpvProcess {
    fn load(&self, url: &str) -> Result<(), String> {
        self.command(json!(["loadfile", url, "replace"]))
            .map(|_| ())
    }

    fn set_paused(&self, is_paused: bool) -> Result<(), String> {
        self.command(json!(["set_property", "pause", is_paused]))
            .map(|_| ())
    }

    fn seek(&self, seconds: f64) -> Result<(), String> {
        self.command(json!(["seek", seconds, "absolute+exact"]))
            .map(|_| ())
    }

    fn set_volume(&self, value: f64) -> Result<(), String> {
        self.command(json!(["set_property", "volume", value * 100.0]))
            .map(|_| ())
    }

    fn apply_processing(
        &self,
        configuration: &MpvProcessingConfiguration,
    ) -> Result<AppliedMpvProcessingConfiguration, String> {
        let replay_gain = match configuration.replay_gain_mode {
            ReplayGainMode::Off => "no",
            ReplayGainMode::Track => "track",
            ReplayGainMode::Album => "album",
        };
        self.command(json!([
            "set_property",
            "volume",
            configuration.volume_percent
        ]))?;
        self.command(json!(["set_property", "replaygain", replay_gain]))?;
        self.command(json!([
            "set_property",
            "af",
            configuration.audio_filters.join(",")
        ]))?;
        let volume_percent = self
            .command(json!(["get_property", "volume"]))?
            .as_f64()
            .ok_or_else(|| "Pinned mpv returned an invalid effective volume.".to_owned())?;
        let replay_gain_mode = match self
            .command(json!(["get_property_string", "replaygain"]))?
            .as_str()
        {
            Some("no") => ReplayGainMode::Off,
            Some("track") => ReplayGainMode::Track,
            Some("album") => ReplayGainMode::Album,
            _ => return Err("Pinned mpv returned an invalid effective ReplayGain mode.".to_owned()),
        };
        let audio_filters = self
            .command(json!(["get_property_string", "af"]))?
            .as_str()
            .ok_or_else(|| "Pinned mpv returned invalid effective audio filters.".to_owned())?
            .to_owned();
        let audio_filters = if audio_filters.is_empty() {
            Vec::new()
        } else {
            audio_filters.split(',').map(str::to_owned).collect()
        };
        Ok(AppliedMpvProcessingConfiguration {
            volume_percent,
            replay_gain_mode,
            audio_filters,
        })
    }

    fn stop(&self) -> Result<(), String> {
        self.command(json!(["stop"])).map(|_| ())
    }

    fn shutdown(&self) {
        self.stop_worker();
    }
}

impl Drop for RealMpvProcess {
    fn drop(&mut self) {
        self.stop_worker();
    }
}

fn ensure_pinned_mpv(binary: &PathBuf) -> Result<(), String> {
    let output = Command::new(binary)
        .arg("--version")
        .output()
        .map_err(|error| format!("Pinned mpv could not start: {error}"))?;
    let version = String::from_utf8_lossy(&output.stdout);
    let pinned_version = MPV_PINNED_VERSION.trim();
    let expected = format!("mpv v{pinned_version}");
    if !output.status.success()
        || !version
            .lines()
            .next()
            .is_some_and(|line| line.starts_with(&expected))
    {
        return Err(format!(
            "Desktop Client requires pinned mpv {pinned_version}; set EARTHLY_AUDIO_MPV_PATH to that executable."
        ));
    }
    Ok(())
}

fn create_private_ipc_directory() -> Result<PathBuf, String> {
    let mut random = [0_u8; 16];
    getrandom::fill(&mut random)
        .map_err(|_| "Private mpv IPC path could not be created.".to_owned())?;
    let name = random
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect::<String>();
    let path = std::env::temp_dir().join(format!("earthly-audio-mpv-{name}"));
    fs::create_dir(&path)
        .map_err(|error| format!("Private mpv IPC directory could not be created: {error}"))?;
    fs::set_permissions(&path, fs::Permissions::from_mode(0o700))
        .map_err(|error| format!("Private mpv IPC permissions could not be set: {error}"))?;
    Ok(path)
}

fn start_mpv_worker(
    binary: &PathBuf,
    ipc_directory: &PathBuf,
    extra_arguments: Vec<String>,
) -> Result<(Sender<MpvWorkerRequest>, JoinHandle<()>, Receiver<MpvEvent>), String> {
    let socket_path = ipc_directory.join("control.sock");
    let mut command = Command::new(binary);
    configure_mpv_command(&mut command, &socket_path, extra_arguments);
    let mut child = command
        .spawn()
        .map_err(|error| format!("Pinned mpv child could not start: {error}"))?;
    wait_for_mpv_socket(&mut child, &socket_path)?;
    fs::set_permissions(&socket_path, fs::Permissions::from_mode(0o600))
        .map_err(|error| format!("Private mpv IPC permissions could not be set: {error}"))?;
    let stream = UnixStream::connect(&socket_path)
        .map_err(|error| format!("Pinned mpv IPC connection failed: {error}"))?;
    stream
        .set_read_timeout(Some(EVENT_POLL_INTERVAL))
        .map_err(|error| format!("Pinned mpv IPC timeout could not be set: {error}"))?;
    Ok(spawn_mpv_worker(child, stream))
}

fn configure_mpv_command(
    command: &mut Command,
    socket_path: &PathBuf,
    extra_arguments: Vec<String>,
) {
    command
        .arg("--no-config")
        .arg("--idle=yes")
        .arg("--no-video")
        .arg("--audio-display=no")
        .arg("--audio-client-name=Earthly Audio")
        .arg("--force-window=no")
        .arg("--terminal=no")
        .arg("--input-terminal=no")
        .arg("--gapless-audio=weak")
        .arg(format!("--volume={}", DEFAULT_VOLUME * 100.0))
        .arg(format!("--input-ipc-server={}", socket_path.display()))
        .args(extra_arguments)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
}

fn wait_for_mpv_socket(child: &mut Child, socket_path: &PathBuf) -> Result<(), String> {
    let deadline = Instant::now() + MPV_START_TIMEOUT;
    while Instant::now() < deadline {
        if socket_path.exists() {
            return Ok(());
        }
        if child
            .try_wait()
            .map_err(|error| format!("Pinned mpv status could not be read: {error}"))?
            .is_some()
        {
            return Err("Pinned mpv exited before opening private IPC.".to_owned());
        }
        thread::sleep(Duration::from_millis(10));
    }
    terminate_mpv(child);
    Err("Pinned mpv did not open private IPC before startup timeout.".to_owned())
}

fn spawn_mpv_worker(
    child: Child,
    stream: UnixStream,
) -> (Sender<MpvWorkerRequest>, JoinHandle<()>, Receiver<MpvEvent>) {
    let (request_sender, request_receiver) = mpsc::channel();
    let (event_sender, event_receiver) = mpsc::channel();
    let worker = thread::spawn(move || {
        run_mpv_worker(child, stream, request_receiver, event_sender);
    });
    (request_sender, worker, event_receiver)
}

fn run_mpv_worker(
    mut child: Child,
    mut stream: UnixStream,
    requests: Receiver<MpvWorkerRequest>,
    events: Sender<MpvEvent>,
) {
    let reader_stream = match stream.try_clone() {
        Ok(stream) => stream,
        Err(error) => {
            send_mpv_event(
                &events,
                MpvEvent::ExitedUnexpectedly(format!("Pinned mpv IPC failed: {error}")),
                "reporting IPC clone failure",
            );
            terminate_mpv(&mut child);
            return;
        }
    };
    let mut reader = BufReader::new(reader_stream);
    let mut request_id = 0_u64;
    loop {
        match requests.recv_timeout(EVENT_POLL_INTERVAL) {
            Ok(MpvWorkerRequest::Command { command, response }) => {
                request_id += 1;
                let result =
                    execute_mpv_command(&mut stream, &mut reader, &events, request_id, command);
                if response.send(result).is_err() {
                    eprintln!(
                        "Pinned mpv command response could not be delivered for request {request_id}: receiver closed."
                    );
                }
            }
            Ok(MpvWorkerRequest::Shutdown) => break,
            Err(RecvTimeoutError::Timeout) => drain_mpv_event(&mut reader, &events),
            Err(RecvTimeoutError::Disconnected) => {
                eprintln!("Pinned mpv worker stopped because its request channel disconnected.");
                break;
            }
        }
        match child.try_wait() {
            Ok(Some(status)) => {
                send_mpv_event(
                    &events,
                    MpvEvent::ExitedUnexpectedly(format!(
                        "Pinned mpv exited unexpectedly with {status}."
                    )),
                    "reporting unexpected process exit",
                );
                return;
            }
            Ok(None) => {}
            Err(error) => {
                send_mpv_event(
                    &events,
                    MpvEvent::ExitedUnexpectedly(format!(
                        "Pinned mpv status could not be read: {error}"
                    )),
                    "reporting process status failure",
                );
                break;
            }
        }
    }
    if let Err(error) = write_mpv_request(&mut stream, 0, json!(["quit"])) {
        eprintln!("Pinned mpv graceful Quit request failed during worker shutdown: {error}");
    }
    terminate_mpv(&mut child);
}

fn execute_mpv_command(
    stream: &mut UnixStream,
    reader: &mut BufReader<UnixStream>,
    events: &Sender<MpvEvent>,
    request_id: u64,
    command: Value,
) -> Result<Value, String> {
    write_mpv_request(stream, request_id, command)?;
    let deadline = Instant::now() + MPV_COMMAND_TIMEOUT;
    while Instant::now() < deadline {
        if let Some(result) = read_mpv_message(reader, events, request_id)? {
            return result;
        }
    }
    Err("Pinned mpv IPC response timed out.".to_owned())
}

fn write_mpv_request(
    stream: &mut UnixStream,
    request_id: u64,
    command: Value,
) -> Result<(), String> {
    let message = json!({ "command": command, "request_id": request_id });
    serde_json::to_writer(&mut *stream, &message)
        .map_err(|error| format!("Pinned mpv command could not be encoded: {error}"))?;
    stream
        .write_all(b"\n")
        .map_err(|error| format!("Pinned mpv IPC write failed: {error}"))
}

fn read_mpv_message(
    reader: &mut BufReader<UnixStream>,
    events: &Sender<MpvEvent>,
    request_id: u64,
) -> Result<Option<Result<Value, String>>, String> {
    let mut line = String::new();
    match reader.read_line(&mut line) {
        Ok(0) => Err("Pinned mpv closed its private IPC connection.".to_owned()),
        Ok(_) => {
            let message: Value = serde_json::from_str(&line)
                .map_err(|error| format!("Pinned mpv returned invalid JSON IPC: {error}"))?;
            if message.get("request_id").and_then(Value::as_u64) == Some(request_id) {
                return Ok(Some(mpv_response_result(&message)));
            }
            emit_mpv_event(events, &message);
            Ok(None)
        }
        Err(error) if matches!(error.kind(), ErrorKind::WouldBlock | ErrorKind::TimedOut) => {
            Ok(None)
        }
        Err(error) => Err(format!("Pinned mpv IPC read failed: {error}")),
    }
}

fn mpv_response_result(message: &Value) -> Result<Value, String> {
    match message.get("error").and_then(Value::as_str) {
        Some("success") => Ok(message.get("data").cloned().unwrap_or(Value::Null)),
        Some(error) => Err(format!("Pinned mpv rejected command: {error}")),
        None => Err("Pinned mpv returned a command response without status.".to_owned()),
    }
}

fn drain_mpv_event(reader: &mut BufReader<UnixStream>, events: &Sender<MpvEvent>) {
    if let Err(error) = read_mpv_message(reader, events, u64::MAX) {
        report_native_playback_error(&format!("mpv event drain failed: {error}"));
        send_mpv_event(
            events,
            MpvEvent::ExitedUnexpectedly(error),
            "reporting IPC event drain failure",
        );
    }
}

fn emit_mpv_event(events: &Sender<MpvEvent>, message: &Value) {
    let event = match message.get("event").and_then(Value::as_str) {
        Some("property-change") => property_event(message),
        Some("end-file") if message.get("reason").and_then(Value::as_str) == Some("eof") => {
            Some(MpvEvent::Ended)
        }
        Some("end-file") if message.get("reason").and_then(Value::as_str) == Some("error") => {
            let detail = message
                .get("file_error")
                .and_then(Value::as_str)
                .unwrap_or("media could not be decoded");
            Some(MpvEvent::Error(format!(
                "Native playback failed: {detail}."
            )))
        }
        _ => None,
    };
    if let Some(event) = event {
        send_mpv_event(events, event, "publishing native playback event");
    }
}

fn send_mpv_event(events: &Sender<MpvEvent>, event: MpvEvent, context: &str) {
    if events.send(event).is_err() {
        eprintln!("Pinned mpv event delivery failed while {context}: receiver closed.");
    }
}

fn property_event(message: &Value) -> Option<MpvEvent> {
    match message.get("name").and_then(Value::as_str) {
        Some("time-pos") => message
            .get("data")
            .and_then(Value::as_f64)
            .map(MpvEvent::Time),
        Some("duration") => message
            .get("data")
            .and_then(Value::as_f64)
            .map(MpvEvent::Duration),
        Some("pause") => message
            .get("data")
            .and_then(Value::as_bool)
            .map(MpvEvent::Paused),
        Some("audio-params") => message
            .get("data")
            .filter(|value| value.is_object())
            .map(ObservedMpvProperties::from_audio_params)
            .map(MpvEvent::Decoder),
        _ => None,
    }
}

fn terminate_mpv(child: &mut Child) {
    let deadline = Instant::now() + MPV_SHUTDOWN_TIMEOUT;
    while Instant::now() < deadline {
        match child.try_wait() {
            Ok(Some(_)) => return,
            Ok(None) => {}
            Err(error) => {
                eprintln!("Pinned mpv status check failed during shutdown: {error}");
                break;
            }
        }
        thread::sleep(Duration::from_millis(10));
    }
    if let Err(error) = child.kill()
        && error.kind() != ErrorKind::InvalidInput
    {
        eprintln!("Pinned mpv forced shutdown failed: {error}");
    }
    if let Err(error) = child.wait() {
        eprintln!("Pinned mpv process reap failed after shutdown: {error}");
    }
}

type StateListener = Arc<dyn Fn(PlaybackSessionState) + Send + Sync>;
type ProcessStarter =
    Arc<dyn Fn() -> Result<(Box<dyn MpvProcessAdapter>, Receiver<MpvEvent>), String> + Send + Sync>;

pub(crate) struct PlaybackController {
    process: Arc<Mutex<Box<dyn MpvProcessAdapter>>>,
    state: Arc<Mutex<PlaybackSessionState>>,
    queue: Arc<Mutex<PlaybackQueueContext>>,
    lifecycle: Arc<Mutex<PlaybackLifecycle>>,
    is_shutdown: Arc<AtomicBool>,
    event_thread: Option<JoinHandle<()>>,
    listener: StateListener,
}

impl PlaybackController {
    pub(crate) fn start_default_with_lifecycle(
        lifecycle: Arc<Mutex<PlaybackLifecycle>>,
        path_observer: Arc<dyn PipeWireObserver>,
        listener: impl Fn(PlaybackSessionState) + Send + Sync + 'static,
    ) -> Result<Self, String> {
        let starter: ProcessStarter = Arc::new(RealMpvProcess::start_default);
        let (process, events) = starter()?;
        Ok(Self::start_internal(
            process,
            events,
            lifecycle.clone(),
            Some(starter),
            Some(path_observer),
            listener,
        ))
    }

    #[cfg(test)]
    pub(crate) fn start(
        process: Box<dyn MpvProcessAdapter>,
        events: Receiver<MpvEvent>,
        listener: impl Fn(PlaybackSessionState) + Send + Sync + 'static,
    ) -> Self {
        Self::start_internal(
            process,
            events,
            Arc::new(Mutex::new(PlaybackLifecycle::new())),
            None,
            None,
            listener,
        )
    }

    #[cfg(test)]
    fn start_with_path_observer(
        process: Box<dyn MpvProcessAdapter>,
        events: Receiver<MpvEvent>,
        path_observer: Arc<dyn PipeWireObserver>,
        listener: impl Fn(PlaybackSessionState) + Send + Sync + 'static,
    ) -> Self {
        Self::start_internal(
            process,
            events,
            Arc::new(Mutex::new(PlaybackLifecycle::new())),
            None,
            Some(path_observer),
            listener,
        )
    }

    #[cfg(test)]
    fn start_recoverable(
        process: Box<dyn MpvProcessAdapter>,
        events: Receiver<MpvEvent>,
        lifecycle: Arc<Mutex<PlaybackLifecycle>>,
        starter: impl Fn() -> Result<(Box<dyn MpvProcessAdapter>, Receiver<MpvEvent>), String>
        + Send
        + Sync
        + 'static,
        listener: impl Fn(PlaybackSessionState) + Send + Sync + 'static,
    ) -> Self {
        Self::start_internal(
            process,
            events,
            lifecycle.clone(),
            Some(Arc::new(starter)),
            None,
            listener,
        )
    }

    fn start_internal(
        process: Box<dyn MpvProcessAdapter>,
        events: Receiver<MpvEvent>,
        lifecycle: Arc<Mutex<PlaybackLifecycle>>,
        starter: Option<ProcessStarter>,
        path_observer: Option<Arc<dyn PipeWireObserver>>,
        listener: impl Fn(PlaybackSessionState) + Send + Sync + 'static,
    ) -> Self {
        let process = Arc::new(Mutex::new(process));
        let state = Arc::new(Mutex::new(PlaybackSessionState::default()));
        let queue = Arc::new(Mutex::new(PlaybackQueueContext::default()));
        let is_shutdown = Arc::new(AtomicBool::new(false));
        let listener: StateListener = Arc::new(listener);
        let event_thread = spawn_event_thread(
            process.clone(),
            state.clone(),
            queue.clone(),
            events,
            is_shutdown.clone(),
            listener.clone(),
            lifecycle.clone(),
            starter,
            path_observer,
        );
        Self {
            process,
            state,
            queue,
            lifecycle,
            is_shutdown,
            event_thread: Some(event_thread),
            listener,
        }
    }

    pub(crate) fn state(&self) -> Result<PlaybackSessionState, PlaybackCommandError> {
        self.state
            .lock()
            .map(|state| state.clone())
            .map_err(|_| PlaybackCommandError::new("Native playback state is unavailable."))
    }

    pub(crate) fn shutdown(&self) {
        self.is_shutdown.store(true, Ordering::Release);
        match self.process.lock() {
            Ok(process) => process.shutdown(),
            Err(_) => {
                report_native_playback_error("mpv shutdown failed: process state is unavailable")
            }
        }
    }

    pub(crate) fn restore_paused(
        &self,
        snapshot: &PlaybackSessionSnapshot,
    ) -> Result<PlaybackSessionState, PlaybackCommandError> {
        let action =
            self.with_process(|process| apply_snapshot_to_process(process, snapshot, false));
        if let Err(error) = action {
            return self.fail_native(error);
        }
        let state = self.update(|state| restore_snapshot_state(state, snapshot))?;
        self.mark_player_stable()?;
        Ok(state)
    }

    pub(crate) fn play(
        &self,
        source: Option<Value>,
    ) -> Result<PlaybackSessionState, PlaybackCommandError> {
        let current = self.state()?;
        let next_source = source.clone().or(current.source.clone());
        let Some(next_source) = next_source else {
            return Ok(current);
        };
        let is_new_source = source.is_some();
        if let Err(error) = self.start_playback(&next_source, is_new_source) {
            return self.fail_native(error);
        }
        let state =
            self.update(|state| update_playing_state(state, next_source.clone(), is_new_source))?;
        self.align_queue_to_source(&next_source)?;
        self.mark_player_stable()?;
        Ok(state)
    }

    fn start_playback(&self, source: &Value, should_load: bool) -> Result<(), String> {
        if !should_load {
            return self.with_process(|process| process.set_paused(false));
        }
        let url = playback_url(source).map_err(|error| error.message)?;
        self.with_process(|process| {
            process.load(&url)?;
            process.set_paused(false)
        })
    }

    pub(crate) fn pause(&self) -> Result<PlaybackSessionState, PlaybackCommandError> {
        if self.state()?.source.is_none() {
            return self.state();
        }
        if let Err(error) = self.with_process(|process| process.set_paused(true)) {
            return self.fail_native(error);
        }
        self.update(|state| state.status = PlaybackStatus::Paused)
    }

    pub(crate) fn stop(&self) -> Result<PlaybackSessionState, PlaybackCommandError> {
        if let Err(error) = self.with_process(|process| process.stop()) {
            return self.fail_native(error);
        }
        self.update(|state| {
            state.source = None;
            state.processing.effective_replay_gain_mode =
                effective_replay_gain_mode(state.processing.replay_gain_mode, None);
            state.status = PlaybackStatus::Idle;
            state.current_time = 0.0;
            state.duration = 0.0;
            state.error = None;
            state.telemetry = PlaybackTelemetry::native_system_output(
                SourceObservation::unknown(),
                ObservedMpvProperties::unknown(),
                None,
                processing_observation(&state.processing),
            );
        })
    }

    pub(crate) fn toggle_play(&self) -> Result<PlaybackSessionState, PlaybackCommandError> {
        if self.state()?.status == PlaybackStatus::Playing {
            self.pause()
        } else {
            self.play(None)
        }
    }

    pub(crate) fn seek(&self, seconds: f64) -> Result<PlaybackSessionState, PlaybackCommandError> {
        if !seconds.is_finite() || seconds < 0.0 {
            return self.fail("Playback seek position must be a non-negative finite number.");
        }
        if let Err(error) = self.with_process(|process| process.seek(seconds)) {
            return self.fail_native(error);
        }
        self.update(|state| state.current_time = seconds)
    }

    pub(crate) fn apply_processing(
        &self,
        mut processing: ProcessingState,
        configuration: &MpvProcessingConfiguration,
    ) -> Result<PlaybackSessionState, PlaybackCommandError> {
        let applied = match self.with_process(|process| process.apply_processing(configuration)) {
            Ok(applied) => applied,
            Err(error) => return self.fail_native(error),
        };
        processing.software_volume = (applied.volume_percent / 100.0).clamp(0.0, 1.0);
        processing.replay_gain_mode = applied.replay_gain_mode;
        processing.equalizer.is_enabled = !applied.audio_filters.is_empty();
        processing.effective_audio_filters = applied.audio_filters;
        self.update(|state| {
            processing.effective_replay_gain_mode =
                effective_replay_gain_mode(applied.replay_gain_mode, state.source.as_ref());
            state.volume = processing.software_volume;
            state.processing = processing.clone();
            state.telemetry.processing = processing_observation(&processing);
        })
    }

    pub(crate) fn set_output_mode(
        &self,
        output_mode: OutputMode,
    ) -> Result<PlaybackSessionState, PlaybackCommandError> {
        self.update(|state| state.output_mode = output_mode)
    }

    pub(crate) fn toggle_shuffle(&self) -> Result<PlaybackSessionState, PlaybackCommandError> {
        let state = self.update(|state| state.shuffle_enabled = !state.shuffle_enabled)?;
        let mut queue = self
            .queue
            .lock()
            .map_err(|_| PlaybackCommandError::new("Playback Queue context is unavailable."))?;
        if state.shuffle_enabled {
            queue.shuffle().map_err(PlaybackCommandError::new)?;
        } else {
            queue.reset_order();
        }
        Ok(state)
    }

    pub(crate) fn sync_queue_context(
        &self,
        sources: Vec<Value>,
        current_index: Option<usize>,
    ) -> Result<PlaybackSessionState, PlaybackCommandError> {
        let is_shuffle_enabled = self.state()?.shuffle_enabled;
        let mut queue = self
            .queue
            .lock()
            .map_err(|_| PlaybackCommandError::new("Playback Queue context is unavailable."))?;
        queue
            .sync(sources, current_index)
            .map_err(PlaybackCommandError::new)?;
        if is_shuffle_enabled {
            queue.shuffle().map_err(PlaybackCommandError::new)?;
        }
        drop(queue);
        self.state()
    }

    pub(crate) fn previous(&self) -> Result<PlaybackSessionState, PlaybackCommandError> {
        self.navigate(QueueDirection::Previous)
    }

    pub(crate) fn next(&self) -> Result<PlaybackSessionState, PlaybackCommandError> {
        self.navigate(QueueDirection::Next)
    }

    #[cfg(test)]
    fn queue_sources(&self) -> Result<Vec<Value>, PlaybackCommandError> {
        self.queue
            .lock()
            .map(|queue| queue.sources())
            .map_err(|_| PlaybackCommandError::new("Playback Queue context is unavailable."))
    }

    pub(crate) fn cycle_repeat_mode(&self) -> Result<PlaybackSessionState, PlaybackCommandError> {
        self.update(|state| {
            state.repeat_mode = match state.repeat_mode {
                RepeatMode::Off => RepeatMode::Once,
                RepeatMode::Once => RepeatMode::Loop,
                RepeatMode::Loop => RepeatMode::Off,
            };
        })
    }

    fn with_process<T>(
        &self,
        action: impl FnOnce(&dyn MpvProcessAdapter) -> Result<T, String>,
    ) -> Result<T, String> {
        let process = self
            .process
            .lock()
            .map_err(|_| "Native mpv process is unavailable.".to_owned())?;
        action(process.as_ref())
    }

    fn update(
        &self,
        change: impl FnOnce(&mut PlaybackSessionState),
    ) -> Result<PlaybackSessionState, PlaybackCommandError> {
        let next = update_shared_state(&self.state, change)?;
        (self.listener)(next.clone());
        Ok(next)
    }

    fn mark_player_stable(&self) -> Result<(), PlaybackCommandError> {
        self.lifecycle
            .lock()
            .map_err(|_| PlaybackCommandError::new("Playback lifecycle state is unavailable."))?
            .player_stabilized();
        Ok(())
    }

    fn navigate(
        &self,
        direction: QueueDirection,
    ) -> Result<PlaybackSessionState, PlaybackCommandError> {
        let should_wrap = self.state()?.repeat_mode == RepeatMode::Loop;
        let source = self
            .queue
            .lock()
            .map_err(|_| PlaybackCommandError::new("Playback Queue context is unavailable."))?
            .adjacent_source(direction, should_wrap);
        match source {
            Some(source) => self.play(Some(source)),
            None => self.state(),
        }
    }

    fn align_queue_to_source(&self, source: &Value) -> Result<(), PlaybackCommandError> {
        self.queue
            .lock()
            .map_err(|_| PlaybackCommandError::new("Playback Queue context is unavailable."))?
            .align_to_source(source)
            .map_err(PlaybackCommandError::new)
    }

    fn fail(
        &self,
        message: impl Into<String>,
    ) -> Result<PlaybackSessionState, PlaybackCommandError> {
        let message = message.into();
        if let Err(error) = self.update(|state| set_error_state(state, message.clone())) {
            eprintln!(
                "Playback failure state could not be published for '{message}': {}",
                error.message
            );
        }
        Err(PlaybackCommandError::new(message))
    }

    fn fail_native(
        &self,
        detail: impl Into<String>,
    ) -> Result<PlaybackSessionState, PlaybackCommandError> {
        report_native_playback_error(&detail.into());
        self.fail(PLAYBACK_FAILED_MESSAGE)
    }
}

fn apply_snapshot_to_process(
    process: &dyn MpvProcessAdapter,
    snapshot: &PlaybackSessionSnapshot,
    should_resume: bool,
) -> Result<(), String> {
    process.set_paused(true)?;
    process.set_volume(snapshot.volume())?;
    if let Some(source) = snapshot.source() {
        let url = playback_url(source).map_err(|error| error.message)?;
        process.load(&url)?;
        process.seek(snapshot.playhead_seconds())?;
        if should_resume {
            process.set_paused(false)?;
        }
    }
    Ok(())
}

fn restore_snapshot_state(state: &mut PlaybackSessionState, snapshot: &PlaybackSessionSnapshot) {
    state.source = snapshot.source().cloned();
    state.status = if state.source.is_some() {
        PlaybackStatus::Paused
    } else {
        PlaybackStatus::Idle
    };
    state.current_time = snapshot.playhead_seconds();
    state.duration = state.source.as_ref().map_or(0.0, track_duration);
    state.volume = snapshot.volume();
    state.shuffle_enabled = snapshot.is_shuffle_enabled();
    state.repeat_mode = match snapshot.repeat_mode() {
        "once" => RepeatMode::Once,
        "loop" => RepeatMode::Loop,
        _ => RepeatMode::Off,
    };
    state.processing.effective_replay_gain_mode =
        effective_replay_gain_mode(state.processing.replay_gain_mode, state.source.as_ref());
    state.telemetry = PlaybackTelemetry::native_system_output(
        state
            .source
            .as_ref()
            .map(SourceObservation::from_playback_source)
            .unwrap_or_else(SourceObservation::unknown),
        ObservedMpvProperties::unknown(),
        None,
        processing_observation(&state.processing),
    );
    state.error = None;
}

impl Drop for PlaybackController {
    fn drop(&mut self) {
        self.shutdown();
        if let Some(event_thread) = self.event_thread.take() {
            if event_thread.join().is_err() {
                eprintln!("Native playback event thread shutdown failed: thread panicked.");
            }
        }
    }
}

fn spawn_event_thread(
    process: Arc<Mutex<Box<dyn MpvProcessAdapter>>>,
    state: Arc<Mutex<PlaybackSessionState>>,
    queue: Arc<Mutex<PlaybackQueueContext>>,
    mut events: Receiver<MpvEvent>,
    is_shutdown: Arc<AtomicBool>,
    listener: StateListener,
    lifecycle: Arc<Mutex<PlaybackLifecycle>>,
    starter: Option<ProcessStarter>,
    path_observer: Option<Arc<dyn PipeWireObserver>>,
) -> JoinHandle<()> {
    thread::spawn(move || {
        let mut last_path_refresh = Instant::now();
        while !is_shutdown.load(Ordering::Acquire) {
            match events.recv_timeout(EVENT_POLL_INTERVAL) {
                Ok(MpvEvent::ExitedUnexpectedly(message)) => {
                    if let Some(next_events) = recover_after_player_exit(
                        &process,
                        &state,
                        &lifecycle,
                        starter.as_ref(),
                        &listener,
                        message,
                    ) {
                        events = next_events;
                    }
                }
                Ok(event) => {
                    if process_mpv_event(
                        &process,
                        &state,
                        &queue,
                        event,
                        &listener,
                        &lifecycle,
                        path_observer.as_deref(),
                        last_path_refresh,
                    ) {
                        last_path_refresh = Instant::now();
                    }
                }
                Err(RecvTimeoutError::Timeout) => {}
                Err(RecvTimeoutError::Disconnected) => {
                    if !is_shutdown.load(Ordering::Acquire) {
                        report_native_playback_error(
                            "playback event channel disconnected unexpectedly",
                        );
                    }
                    break;
                }
            }
        }
    })
}

fn process_mpv_event(
    process: &Arc<Mutex<Box<dyn MpvProcessAdapter>>>,
    state: &Arc<Mutex<PlaybackSessionState>>,
    queue: &Arc<Mutex<PlaybackQueueContext>>,
    event: MpvEvent,
    listener: &StateListener,
    lifecycle: &Arc<Mutex<PlaybackLifecycle>>,
    path_observer: Option<&dyn PipeWireObserver>,
    last_path_refresh: Instant,
) -> bool {
    let is_decoder_readiness = matches!(event, MpvEvent::Decoder(_));
    let is_stable_playback = is_stable_playback_event(&event);
    let should_refresh_path = matches!(event, MpvEvent::Time(_))
        && last_path_refresh.elapsed() >= PATH_REFRESH_INTERVAL
        && path_observer.is_some();
    match handle_mpv_event(process, state, queue, event, listener, path_observer) {
        Err(error) => eprintln!(
            "Native playback event state update failed: {}",
            error.message
        ),
        Ok(()) if is_stable_playback => mark_recovered_player_stable(state, lifecycle, listener),
        Ok(()) => {}
    }
    if should_refresh_path && !is_decoder_readiness {
        refresh_pipewire_telemetry(state, listener, path_observer);
    }
    is_decoder_readiness || should_refresh_path
}

fn is_stable_playback_event(event: &MpvEvent) -> bool {
    match event {
        MpvEvent::Decoder(_) => true,
        MpvEvent::Time(value) => value.is_finite() && *value >= 0.0,
        _ => false,
    }
}

fn recover_after_player_exit(
    process: &Arc<Mutex<Box<dyn MpvProcessAdapter>>>,
    state: &Arc<Mutex<PlaybackSessionState>>,
    lifecycle: &Arc<Mutex<PlaybackLifecycle>>,
    starter: Option<&ProcessStarter>,
    listener: &StateListener,
    message: String,
) -> Option<Receiver<MpvEvent>> {
    report_native_playback_error(&message);
    match try_recover_after_player_exit(process, state, lifecycle, starter, listener) {
        Ok(events) => events,
        Err(failure) => {
            report_native_playback_error(&format!(
                "recovery failed [{}]: {}",
                failure.code, failure.diagnostic
            ));
            if let Err(error) =
                publish_lifecycle_failure(state, listener, failure.code, failure.message)
            {
                eprintln!(
                    "Native playback recovery error could not be published: {}",
                    error.message
                );
            }
            None
        }
    }
}

struct RecoveryFailure {
    code: &'static str,
    message: String,
    diagnostic: String,
}

impl RecoveryFailure {
    fn internal(code: &'static str, diagnostic: impl Into<String>) -> Self {
        Self {
            code,
            message: recovery_public_message(code).to_owned(),
            diagnostic: diagnostic.into(),
        }
    }

    fn actionable(code: &'static str, message: String) -> Self {
        Self {
            code,
            diagnostic: message.clone(),
            message,
        }
    }
}

fn recovery_public_message(code: &str) -> &'static str {
    match code {
        "mpv-recovery-state-unavailable" => {
            "Native playback could not recover because Playback Session state is unavailable. Restart the Desktop Client."
        }
        "mpv-recovery-snapshot-failed" => {
            "Native playback could not preserve the interrupted session. Quit and reopen the Desktop Client to try again."
        }
        "mpv-recovery-lifecycle-unavailable" => {
            "Native playback could not recover because lifecycle state is unavailable. Restart the Desktop Client."
        }
        "mpv-restart-unavailable" => {
            "Native playback cannot restart in this session. Restart the Desktop Client."
        }
        "mpv-recovery-transition-invalid" => {
            "Native playback could not complete recovery. Quit and reopen the Desktop Client to try again."
        }
        _ => "Native playback could not restart. Quit and reopen the Desktop Client to try again.",
    }
}

fn try_recover_after_player_exit(
    process: &Arc<Mutex<Box<dyn MpvProcessAdapter>>>,
    state: &Arc<Mutex<PlaybackSessionState>>,
    lifecycle: &Arc<Mutex<PlaybackLifecycle>>,
    starter: Option<&ProcessStarter>,
    listener: &StateListener,
) -> Result<Option<Receiver<MpvEvent>>, RecoveryFailure> {
    let (snapshot, should_resume) = interrupted_session_snapshot(state)?;
    let transition = lifecycle
        .lock()
        .map_err(|_| {
            RecoveryFailure::internal(
                "mpv-recovery-lifecycle-unavailable",
                "Native playback could not recover because lifecycle state is unavailable. Restart the Desktop Client.",
            )
        })?
        .unexpected_player_exit(&snapshot, should_resume);
    apply_recovery_transition(process, state, starter, listener, transition.action)
}

fn interrupted_session_snapshot(
    state: &Arc<Mutex<PlaybackSessionState>>,
) -> Result<(PlaybackSessionSnapshot, bool), RecoveryFailure> {
    let current = state.lock().map_err(|_| {
        RecoveryFailure::internal(
            "mpv-recovery-state-unavailable",
            "Native playback could not recover because its Playback Session state is unavailable. Restart the Desktop Client.",
        )
    })?.clone();
    let snapshot = PlaybackSessionSnapshot::from_serializable_state(&current).map_err(|error| {
        RecoveryFailure::internal(
            "mpv-recovery-snapshot-failed",
            format!("Native playback could not snapshot the interrupted session: {error}"),
        )
    })?;
    Ok((snapshot, current.status == PlaybackStatus::Playing))
}

fn apply_recovery_transition(
    process: &Arc<Mutex<Box<dyn MpvProcessAdapter>>>,
    state: &Arc<Mutex<PlaybackSessionState>>,
    starter: Option<&ProcessStarter>,
    listener: &StateListener,
    action: PlaybackLifecycleAction,
) -> Result<Option<Receiver<MpvEvent>>, RecoveryFailure> {
    match action {
        PlaybackLifecycleAction::RestartPlayer {
            snapshot,
            should_resume,
        } => {
            let starter = starter.ok_or_else(|| {
                RecoveryFailure::internal(
                    "mpv-restart-unavailable",
                    "Native playback cannot restart mpv in this session. Restart the Desktop Client.",
                )
            })?;
            restart_player(process, state, starter, listener, &snapshot, should_resume).map(Some)
        }
        PlaybackLifecycleAction::SurfaceActionableError(error) => {
            Err(RecoveryFailure::actionable(error.code, error.message))
        }
        invalid_action => Err(RecoveryFailure::internal(
            "mpv-recovery-transition-invalid",
            format!(
                "Native playback recovery produced an invalid lifecycle action: {invalid_action:?}"
            ),
        )),
    }
}

fn restart_player(
    process: &Arc<Mutex<Box<dyn MpvProcessAdapter>>>,
    state: &Arc<Mutex<PlaybackSessionState>>,
    starter: &ProcessStarter,
    listener: &StateListener,
    snapshot: &PlaybackSessionSnapshot,
    should_resume: bool,
) -> Result<Receiver<MpvEvent>, RecoveryFailure> {
    let (next_process, next_events) =
        starter().map_err(|message| RecoveryFailure::internal("mpv-restart-failed", message))?;
    if let Err(message) = apply_snapshot_to_process(next_process.as_ref(), snapshot, should_resume)
    {
        return Err(RecoveryFailure::internal("mpv-restart-failed", message));
    }
    *process.lock().map_err(|_| {
        RecoveryFailure::internal(
            "mpv-restart-failed",
            "Native mpv process state is unavailable.",
        )
    })? = next_process;
    let next = update_shared_state(state, |state| {
        restore_snapshot_state(state, snapshot);
        if should_resume && state.source.is_some() {
            state.status = PlaybackStatus::Playing;
        }
    })
    .map_err(|error| RecoveryFailure::internal("mpv-recovery-state-unavailable", error.message))?;
    listener(next);
    Ok(next_events)
}

fn publish_lifecycle_failure(
    state: &Arc<Mutex<PlaybackSessionState>>,
    listener: &StateListener,
    code: &str,
    message: String,
) -> Result<(), PlaybackCommandError> {
    let next = update_shared_state(state, |state| {
        state.status = PlaybackStatus::Error;
        state.error = Some(PlaybackStateError {
            code: code.to_owned(),
            message: message.clone(),
        });
    })?;
    listener(next);
    Ok(())
}

fn mark_recovered_player_stable(
    state: &Arc<Mutex<PlaybackSessionState>>,
    lifecycle: &Arc<Mutex<PlaybackLifecycle>>,
    listener: &StateListener,
) {
    if let Ok(mut lifecycle) = lifecycle.lock() {
        lifecycle.player_stabilized();
        return;
    }
    let message = "Native playback became stable, but recovery lifecycle state is unavailable. Restart the Desktop Client.".to_owned();
    report_native_playback_error(&message);
    if let Err(error) = publish_lifecycle_failure(
        state,
        listener,
        "mpv-recovery-lifecycle-unavailable",
        message,
    ) {
        eprintln!(
            "Native playback lifecycle failure could not be published: {}",
            error.message
        );
    }
}

fn handle_mpv_event(
    process: &Arc<Mutex<Box<dyn MpvProcessAdapter>>>,
    state: &Arc<Mutex<PlaybackSessionState>>,
    queue: &Arc<Mutex<PlaybackQueueContext>>,
    event: MpvEvent,
    listener: &StateListener,
    path_observer: Option<&dyn PipeWireObserver>,
) -> Result<(), PlaybackCommandError> {
    let result = match event {
        MpvEvent::Time(value) if value.is_finite() && value >= 0.0 => {
            update_shared_state(state, |state| state.current_time = value)
        }
        MpvEvent::Duration(value) if value.is_finite() && value > 0.0 => {
            update_shared_state(state, |state| state.duration = value)
        }
        MpvEvent::Paused(is_paused) => update_paused_state(state, is_paused),
        MpvEvent::Decoder(decoder) => update_decoder_telemetry(state, decoder, path_observer),
        MpvEvent::Ended => handle_ended_event(process, state, queue),
        MpvEvent::Error(message) => {
            report_native_playback_error(&message);
            update_shared_state(state, |state| {
                set_error_state(state, PLAYBACK_FAILED_MESSAGE.to_owned())
            })
        }
        MpvEvent::ExitedUnexpectedly(_) => return Ok(()),
        MpvEvent::Time(_) | MpvEvent::Duration(_) => return Ok(()),
    };
    let next = result?;
    listener(next);
    Ok(())
}

fn update_paused_state(
    state: &Arc<Mutex<PlaybackSessionState>>,
    is_paused: bool,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    update_shared_state(state, |state| {
        if state.source.is_some() && state.status != PlaybackStatus::Ended {
            state.status = if is_paused {
                PlaybackStatus::Paused
            } else {
                PlaybackStatus::Playing
            };
        }
    })
}

fn update_decoder_telemetry(
    state: &Arc<Mutex<PlaybackSessionState>>,
    decoder: ObservedMpvProperties,
    path_observer: Option<&dyn PipeWireObserver>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    let pipewire = observe_pipewire_path(path_observer);
    update_shared_state(state, |state| {
        state.telemetry = PlaybackTelemetry::native_system_output(
            source_observation(state.source.as_ref()),
            decoder.clone(),
            pipewire.clone(),
            processing_observation(&state.processing),
        );
    })
}

fn source_observation(source: Option<&Value>) -> SourceObservation {
    source
        .map(SourceObservation::from_playback_source)
        .unwrap_or_else(SourceObservation::unknown)
}

fn observe_pipewire_path(observer: Option<&dyn PipeWireObserver>) -> Option<PipeWireObservation> {
    match observer?.observe() {
        Ok(observation) => observation,
        Err(error) => {
            eprintln!("PipeWire playback observation failed: {error}");
            None
        }
    }
}

fn refresh_pipewire_telemetry(
    state: &Arc<Mutex<PlaybackSessionState>>,
    listener: &StateListener,
    observer: Option<&dyn PipeWireObserver>,
) {
    let pipewire = observe_pipewire_path(observer);
    match update_shared_state(state, |state| {
        state.telemetry = PlaybackTelemetry::native_system_output(
            source_observation(state.source.as_ref()),
            state.telemetry.decoder.clone(),
            pipewire.clone(),
            processing_observation(&state.processing),
        );
    }) {
        Ok(next) => listener(next),
        Err(error) => eprintln!(
            "PipeWire telemetry refresh could not update playback state: {}",
            error.message
        ),
    }
}

fn handle_ended_event(
    process: &Arc<Mutex<Box<dyn MpvProcessAdapter>>>,
    state: &Arc<Mutex<PlaybackSessionState>>,
    queue: &Arc<Mutex<PlaybackQueueContext>>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    let repeat_mode = state
        .lock()
        .map_err(|_| PlaybackCommandError::new("Native playback state is unavailable."))?
        .repeat_mode
        .clone();
    if repeat_mode == RepeatMode::Once {
        return repeat_current_source(process, state);
    }
    let next_source = queue
        .lock()
        .map_err(|_| PlaybackCommandError::new("Playback Queue context is unavailable."))?
        .adjacent_source(QueueDirection::Next, repeat_mode == RepeatMode::Loop);
    if let Some(source) = next_source {
        return load_advanced_source(process, state, queue, source);
    }
    update_shared_state(state, |state| {
        state.status = PlaybackStatus::Ended;
    })
}

fn repeat_current_source(
    process: &Arc<Mutex<Box<dyn MpvProcessAdapter>>>,
    state: &Arc<Mutex<PlaybackSessionState>>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    let result = process
        .lock()
        .map_err(|_| "Native mpv process is unavailable.".to_owned())
        .and_then(|process| {
            process.seek(0.0)?;
            process.set_paused(false)
        });
    if let Err(message) = result {
        return publish_native_command_failure(state, message);
    }
    update_shared_state(state, |state| {
        state.current_time = 0.0;
        state.status = PlaybackStatus::Playing;
        state.repeat_mode = RepeatMode::Off;
    })
}

fn load_advanced_source(
    process: &Arc<Mutex<Box<dyn MpvProcessAdapter>>>,
    state: &Arc<Mutex<PlaybackSessionState>>,
    queue: &Arc<Mutex<PlaybackQueueContext>>,
    source: Value,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    let url = playback_url(&source)?;
    let result = process
        .lock()
        .map_err(|_| "Native mpv process is unavailable.".to_owned())
        .and_then(|process| {
            process.load(&url)?;
            process.set_paused(false)
        });
    if let Err(message) = result {
        return publish_native_command_failure(state, message);
    }
    queue
        .lock()
        .map_err(|_| PlaybackCommandError::new("Playback Queue context is unavailable."))?
        .align_to_source(&source)
        .map_err(PlaybackCommandError::new)?;
    update_shared_state(state, |state| set_playing_source(state, source.clone()))
}

fn set_playing_source(state: &mut PlaybackSessionState, source: Value) {
    state.duration = track_duration(&source);
    state.processing.effective_replay_gain_mode =
        effective_replay_gain_mode(state.processing.replay_gain_mode, Some(&source));
    state.telemetry = PlaybackTelemetry::native_system_output(
        SourceObservation::from_playback_source(&source),
        ObservedMpvProperties::unknown(),
        None,
        processing_observation(&state.processing),
    );
    state.source = Some(source);
    state.status = PlaybackStatus::Playing;
    state.current_time = 0.0;
    state.error = None;
}

fn update_playing_state(state: &mut PlaybackSessionState, source: Value, is_new_source: bool) {
    if is_new_source {
        set_playing_source(state, source);
        return;
    }
    state.source = Some(source);
    state.status = PlaybackStatus::Playing;
    state.error = None;
}

fn publish_native_command_failure(
    state: &Arc<Mutex<PlaybackSessionState>>,
    message: String,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    report_native_playback_error(&message);
    update_shared_state(state, |state| {
        set_error_state(state, PLAYBACK_FAILED_MESSAGE.to_owned())
    })
}

fn update_shared_state(
    state: &Arc<Mutex<PlaybackSessionState>>,
    change: impl FnOnce(&mut PlaybackSessionState),
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    let mut state = state
        .lock()
        .map_err(|_| PlaybackCommandError::new("Native playback state is unavailable."))?;
    change(&mut state);
    Ok(state.clone())
}

fn set_error_state(state: &mut PlaybackSessionState, message: String) {
    state.status = PlaybackStatus::Error;
    state.error = Some(PlaybackStateError {
        code: "playback-failed".to_owned(),
        message,
    });
}

fn report_native_playback_error(detail: &str) {
    eprintln!("Native playback process failed: {detail}");
}

fn playback_url(source: &Value) -> Result<String, PlaybackCommandError> {
    let parsed = PlaybackSource::parse(source).map_err(PlaybackCommandError::new)?;
    Ok(parsed
        .playback_url()
        .map_err(PlaybackCommandError::new)?
        .to_owned())
}

fn track_duration(source: &Value) -> f64 {
    match PlaybackSource::parse(source) {
        Ok(source) => source.duration_seconds(),
        Err(error) => {
            eprintln!("Playback Source duration could not be inspected: {error}");
            0.0
        }
    }
}

fn processing_observation(state: &ProcessingState) -> ProcessingObservation {
    ProcessingObservation {
        profile: match state.profile {
            crate::processing::ProcessingProfile::Direct => "direct",
            crate::processing::ProcessingProfile::Processed => "processed",
        }
        .to_owned(),
        software_volume: Some(state.software_volume),
        replay_gain_mode: match state.replay_gain_mode {
            ReplayGainMode::Off => "off",
            ReplayGainMode::Track => "track",
            ReplayGainMode::Album => "album",
        }
        .to_owned(),
        effective_replay_gain_mode: match state.effective_replay_gain_mode {
            EffectiveReplayGainMode::Off => "off",
            EffectiveReplayGainMode::Track => "track",
            EffectiveReplayGainMode::Album => "album",
            EffectiveReplayGainMode::TrackFallback => "track-fallback",
            EffectiveReplayGainMode::Unavailable => "unavailable",
            EffectiveReplayGainMode::Unknown => "unknown",
        }
        .to_owned(),
        is_equalizer_enabled: Some(state.equalizer.is_enabled),
    }
}

fn effective_replay_gain_mode(
    mode: ReplayGainMode,
    source: Option<&Value>,
) -> EffectiveReplayGainMode {
    let track_gain = source
        .and_then(|value| value.pointer("/track/replayGain/trackGainDb"))
        .is_some_and(Value::is_number);
    let album_gain = source
        .and_then(|value| value.pointer("/track/replayGain/albumGainDb"))
        .is_some_and(Value::is_number);
    match mode {
        ReplayGainMode::Off => EffectiveReplayGainMode::Off,
        ReplayGainMode::Track if track_gain => EffectiveReplayGainMode::Track,
        ReplayGainMode::Album if album_gain => EffectiveReplayGainMode::Album,
        ReplayGainMode::Album if track_gain => EffectiveReplayGainMode::TrackFallback,
        ReplayGainMode::Track | ReplayGainMode::Album => EffectiveReplayGainMode::Unavailable,
    }
}

#[cfg(test)]
mod tests {
    use super::{MpvEvent, MpvProcessAdapter, PlaybackController, PlaybackStatus, RealMpvProcess};
    use crate::playback_app_actions::{
        DesktopPlaybackAction, DesktopPlaybackShell, dispatch_desktop_playback_action,
    };
    use crate::playback_lifecycle::{
        PlaybackLifecycle, PlaybackSessionSnapshot, PlaybackSnapshotStore,
    };
    use crate::playback_test_support::{ControlledStreamServer, generate_audio_fixture};
    use crate::processing::{
        AppliedMpvProcessingConfiguration, EffectiveReplayGainMode, MpvProcessingConfiguration,
        OutputMode, ProcessingState, ReplayGainMode,
    };
    use crate::telemetry::{
        AudioFormatObservation, ObservedMpvProperties, PipeWireObservation, PipeWireObserver,
        SourceObservation,
    };
    use serde_json::json;
    use std::fs;
    use std::os::unix::fs::PermissionsExt;
    use std::path::PathBuf;
    use std::process::Command;
    use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
    use std::sync::{Arc, Mutex};
    use std::thread;
    use std::time::{Duration, Instant};

    struct FakeMpvProcess {
        loaded_url: Arc<Mutex<Option<String>>>,
        is_shutdown: Arc<AtomicBool>,
        load_error: Option<String>,
    }

    #[derive(Default)]
    struct TestDesktopPlaybackShell {
        is_hidden: AtomicBool,
        has_exited: AtomicBool,
    }

    impl DesktopPlaybackShell for TestDesktopPlaybackShell {
        fn show_main_window(&self) -> Result<(), String> {
            Ok(())
        }

        fn hide_main_window(&self) -> Result<(), String> {
            self.is_hidden.store(true, Ordering::Release);
            Ok(())
        }

        fn exit(&self) {
            self.has_exited.store(true, Ordering::Release);
        }
    }

    struct ObservedPipeWirePath {
        calls: AtomicUsize,
    }

    struct ObservedProcessingMpv {
        requested: Arc<Mutex<Option<MpvProcessingConfiguration>>>,
    }

    impl MpvProcessAdapter for ObservedProcessingMpv {
        fn load(&self, _url: &str) -> Result<(), String> {
            Ok(())
        }

        fn set_paused(&self, _is_paused: bool) -> Result<(), String> {
            Ok(())
        }

        fn seek(&self, _seconds: f64) -> Result<(), String> {
            Ok(())
        }

        fn set_volume(&self, _value: f64) -> Result<(), String> {
            Ok(())
        }

        fn apply_processing(
            &self,
            configuration: &MpvProcessingConfiguration,
        ) -> Result<AppliedMpvProcessingConfiguration, String> {
            *self.requested.lock().expect("requested processing") = Some(configuration.clone());
            Ok(AppliedMpvProcessingConfiguration {
                volume_percent: 37.0,
                replay_gain_mode: ReplayGainMode::Track,
                audio_filters: vec!["lavfi=[equalizer=f=1000:g=1.5]".to_owned()],
            })
        }

        fn stop(&self) -> Result<(), String> {
            Ok(())
        }

        fn shutdown(&self) {}
    }

    impl PipeWireObserver for ObservedPipeWirePath {
        fn observe(&self) -> Result<Option<PipeWireObservation>, String> {
            self.calls.fetch_add(1, Ordering::Relaxed);
            Ok(Some(PipeWireObservation {
                graph_format: AudioFormatObservation {
                    sample_rate_hz: Some(96_000),
                    bit_depth: Some(24),
                    channels: Some(2),
                },
                is_graph_resampling: Some(false),
                device_name: Some("Routed USB DAC".to_owned()),
                device_format: AudioFormatObservation {
                    sample_rate_hz: Some(96_000),
                    bit_depth: Some(24),
                    channels: Some(2),
                },
                is_device_resampling: Some(false),
            }))
        }
    }

    #[derive(Default)]
    struct RestoredProcessState {
        loaded_url: Option<String>,
        is_paused: bool,
        playhead_seconds: f64,
        volume: f64,
    }

    struct RestorableMpvProcess {
        state: Arc<Mutex<RestoredProcessState>>,
    }

    impl MpvProcessAdapter for RestorableMpvProcess {
        fn load(&self, url: &str) -> Result<(), String> {
            self.state.lock().expect("process state").loaded_url = Some(url.to_owned());
            Ok(())
        }

        fn set_paused(&self, is_paused: bool) -> Result<(), String> {
            self.state.lock().expect("process state").is_paused = is_paused;
            Ok(())
        }

        fn seek(&self, seconds: f64) -> Result<(), String> {
            self.state.lock().expect("process state").playhead_seconds = seconds;
            Ok(())
        }

        fn set_volume(&self, value: f64) -> Result<(), String> {
            self.state.lock().expect("process state").volume = value;
            Ok(())
        }

        fn stop(&self) -> Result<(), String> {
            Ok(())
        }

        fn shutdown(&self) {}
    }

    impl MpvProcessAdapter for FakeMpvProcess {
        fn load(&self, url: &str) -> Result<(), String> {
            if let Some(error) = self.load_error.as_ref() {
                return Err(error.clone());
            }
            *self.loaded_url.lock().expect("loaded URL") = Some(url.to_owned());
            Ok(())
        }

        fn set_paused(&self, _is_paused: bool) -> Result<(), String> {
            Ok(())
        }

        fn seek(&self, _seconds: f64) -> Result<(), String> {
            Ok(())
        }

        fn set_volume(&self, _value: f64) -> Result<(), String> {
            Ok(())
        }

        fn stop(&self) -> Result<(), String> {
            Ok(())
        }

        fn shutdown(&self) {
            self.is_shutdown.store(true, Ordering::Release);
        }
    }

    #[test]
    fn track_playback_projects_mpv_timing_events() {
        let (event_sender, event_receiver) = std::sync::mpsc::channel();
        let loaded_url = Arc::new(Mutex::new(None));
        let is_shutdown = Arc::new(AtomicBool::new(false));
        let process = FakeMpvProcess {
            loaded_url: loaded_url.clone(),
            is_shutdown,
            load_error: None,
        };
        let controller = PlaybackController::start(Box::new(process), event_receiver, |_| {});
        let source = json!({
            "type": "track",
            "track": {
                "id": "track-1",
                "title": "Track 1",
                "durationMs": 120000
            },
            "playbackUrl": "http://127.0.0.1:43129/token/api/v1/tracks/track-1/stream"
        });

        let playing = controller.play(Some(source.clone())).expect("play Track");
        event_sender.send(MpvEvent::Time(15.5)).expect("time event");
        event_sender
            .send(MpvEvent::Duration(121.25))
            .expect("duration event");
        let observed = wait_for_state(&controller, |state| {
            state.current_time == 15.5 && state.duration == 121.25
        });

        assert_eq!(playing.status, PlaybackStatus::Playing);
        assert_eq!(playing.source, Some(source));
        assert_eq!(playing.duration, 120.0);
        assert_eq!(observed.current_time, 15.5);
        assert_eq!(observed.duration, 121.25);
        assert_eq!(
            loaded_url.lock().expect("loaded URL").as_deref(),
            Some("http://127.0.0.1:43129/token/api/v1/tracks/track-1/stream")
        );
    }

    #[test]
    fn replacing_or_stopping_a_source_clears_stale_path_observations() {
        let (event_sender, event_receiver) = std::sync::mpsc::channel();
        let process = FakeMpvProcess {
            loaded_url: Arc::new(Mutex::new(None)),
            is_shutdown: Arc::new(AtomicBool::new(false)),
            load_error: None,
        };
        let controller = PlaybackController::start(Box::new(process), event_receiver, |_| {});
        let first_source = json!({
            "type": "track",
            "track": { "id": "track-1", "title": "Track 1", "durationMs": 120000 },
            "playbackUrl": "http://127.0.0.1:43129/token/api/v1/tracks/track-1/stream"
        });
        let next_source = json!({
            "type": "track",
            "track": { "id": "track-2", "title": "Track 2", "durationMs": 90000 },
            "playbackUrl": "http://127.0.0.1:43129/token/api/v1/tracks/track-2/stream"
        });

        controller
            .play(Some(first_source))
            .expect("play first Track");
        event_sender
            .send(MpvEvent::Decoder(ObservedMpvProperties::from_audio_params(
                &json!({ "format": "s24", "samplerate": 96000, "channel-count": 2 }),
            )))
            .expect("decoder event");
        wait_for_state(&controller, |state| {
            state.telemetry.decoder.format.sample_rate_hz == Some(96_000)
        });

        let replaced = controller
            .play(Some(next_source))
            .expect("play replacement Track");
        assert_eq!(replaced.telemetry.decoder, ObservedMpvProperties::unknown());

        let stopped = controller.stop().expect("stop playback");
        assert_eq!(stopped.telemetry.source, SourceObservation::unknown());
        assert_eq!(stopped.telemetry.decoder, ObservedMpvProperties::unknown());
        assert_eq!(stopped.telemetry.system.format.sample_rate_hz, None);
        assert_eq!(stopped.telemetry.device.name, None);
    }

    #[test]
    fn pipewire_path_is_sampled_after_decoder_readiness_and_refreshed_during_playback() {
        let (event_sender, event_receiver) = std::sync::mpsc::channel();
        let process = FakeMpvProcess {
            loaded_url: Arc::new(Mutex::new(None)),
            is_shutdown: Arc::new(AtomicBool::new(false)),
            load_error: None,
        };
        let observer = Arc::new(ObservedPipeWirePath {
            calls: AtomicUsize::new(0),
        });
        let controller = PlaybackController::start_with_path_observer(
            Box::new(process),
            event_receiver,
            observer.clone(),
            |_| {},
        );
        let source = json!({
            "type": "track",
            "track": { "id": "track-1", "title": "Track 1", "durationMs": 120000 },
            "playbackUrl": "http://127.0.0.1:43129/token/api/v1/tracks/track-1/stream"
        });

        let loading = controller.play(Some(source)).expect("load Track");
        assert_eq!(observer.calls.load(Ordering::Relaxed), 0);
        assert_eq!(loading.telemetry.device.name, None);

        event_sender
            .send(MpvEvent::Decoder(ObservedMpvProperties::from_audio_params(
                &json!({ "format": "s24", "samplerate": 96000, "channel-count": 2 }),
            )))
            .expect("decoder readiness");
        let observed = wait_for_state(&controller, |state| {
            state.telemetry.device.name.as_deref() == Some("Routed USB DAC")
        });

        assert_eq!(observer.calls.load(Ordering::Relaxed), 1);
        assert_eq!(observed.telemetry.system.format.bit_depth, Some(24));

        thread::sleep(super::PATH_REFRESH_INTERVAL + Duration::from_millis(10));
        event_sender.send(MpvEvent::Time(1.0)).expect("time event");
        wait_for_state(&controller, |_| observer.calls.load(Ordering::Relaxed) == 2);
        assert_eq!(observer.calls.load(Ordering::Relaxed), 2);
    }

    #[test]
    fn processing_state_reports_effective_mpv_results_without_changing_output_mode() {
        let (_event_sender, event_receiver) = std::sync::mpsc::channel();
        let requested = Arc::new(Mutex::new(None));
        let controller = PlaybackController::start(
            Box::new(ObservedProcessingMpv {
                requested: requested.clone(),
            }),
            event_receiver,
            |_| {},
        );
        let mut processing = ProcessingState::default();
        processing.profile = crate::processing::ProcessingProfile::Processed;
        processing.software_volume = 0.4;
        processing.replay_gain_mode = ReplayGainMode::Album;
        processing.equalizer.is_enabled = true;
        let configuration = MpvProcessingConfiguration {
            volume_percent: 40.0,
            replay_gain_mode: ReplayGainMode::Album,
            audio_filters: vec!["lavfi=[equalizer=f=1000:g=2]".to_owned()],
        };
        let output_mode = controller.state().expect("initial state").output_mode;
        assert_eq!(output_mode, OutputMode::System);

        let observed = controller
            .apply_processing(processing, &configuration)
            .expect("apply processing");

        assert_eq!(
            *requested.lock().expect("requested processing"),
            Some(configuration)
        );
        assert_eq!(observed.processing.software_volume, 0.37);
        assert_eq!(observed.processing.replay_gain_mode, ReplayGainMode::Track);
        assert!(observed.processing.equalizer.is_enabled);
        assert_eq!(
            observed.processing.effective_audio_filters,
            ["lavfi=[equalizer=f=1000:g=1.5]"]
        );
        assert_eq!(observed.telemetry.processing.software_volume, Some(0.37));
        assert_eq!(observed.output_mode, output_mode);
    }

    #[test]
    fn album_replay_gain_publishes_the_effective_track_fallback() {
        let (_event_sender, event_receiver) = std::sync::mpsc::channel();
        let controller = PlaybackController::start(
            Box::new(FakeMpvProcess {
                loaded_url: Arc::new(Mutex::new(None)),
                is_shutdown: Arc::new(AtomicBool::new(false)),
                load_error: None,
            }),
            event_receiver,
            |_| {},
        );
        controller
            .play(Some(json!({
                "type": "track",
                "track": {
                    "id": "track-1",
                    "title": "Track 1",
                    "durationMs": 120000,
                    "replayGain": {
                        "trackGainDb": -7.25,
                        "trackPeak": 0.9,
                        "albumGainDb": null,
                        "albumPeak": null
                    }
                },
                "playbackUrl": "http://127.0.0.1:43129/token/api/v1/tracks/track-1/stream"
            })))
            .expect("play Track with ReplayGain metadata");
        let mut processing = ProcessingState::default();
        processing.profile = crate::processing::ProcessingProfile::Processed;
        processing.replay_gain_mode = ReplayGainMode::Album;

        let observed = controller
            .apply_processing(
                processing,
                &MpvProcessingConfiguration {
                    volume_percent: 100.0,
                    replay_gain_mode: ReplayGainMode::Album,
                    audio_filters: Vec::new(),
                },
            )
            .expect("apply Album ReplayGain");

        assert_eq!(
            observed.processing.effective_replay_gain_mode,
            EffectiveReplayGainMode::TrackFallback
        );
        assert_eq!(
            observed.telemetry.processing.effective_replay_gain_mode,
            "track-fallback"
        );

        let unavailable = controller
            .play(Some(json!({
                "type": "radio-station",
                "station": {
                    "id": "radio-1",
                    "name": "Radio",
                    "streamUrl": "https://example.com/radio"
                },
                "playbackUrl": "http://127.0.0.1:43129/token/api/v1/radio/stations/radio-1/stream",
                "sourceUrl": "https://example.com/radio"
            })))
            .expect("switch to source without ReplayGain metadata");
        assert_eq!(
            unavailable.processing.effective_replay_gain_mode,
            EffectiveReplayGainMode::Unavailable
        );
        assert_eq!(
            unavailable.telemetry.processing.effective_replay_gain_mode,
            "unavailable"
        );
    }

    #[test]
    fn persisted_session_restores_source_and_playhead_paused() {
        let (_event_sender, event_receiver) = std::sync::mpsc::channel();
        let process_state = Arc::new(Mutex::new(RestoredProcessState::default()));
        let process = RestorableMpvProcess {
            state: process_state.clone(),
        };
        let controller = PlaybackController::start(Box::new(process), event_receiver, |_| {});
        let source = json!({
            "type": "track",
            "track": { "id": "track-1", "title": "Track 1", "durationMs": 120000 },
            "playbackUrl": "http://127.0.0.1:43129/token/api/v1/tracks/track-1/stream"
        });
        let snapshot = PlaybackSessionSnapshot::new(Some(source.clone()), 41.5, 0.65, true, "loop")
            .expect("valid snapshot");

        let restored = controller
            .restore_paused(&snapshot)
            .expect("restore paused session");
        let process = process_state.lock().expect("process state");

        assert_eq!(restored.source, Some(source));
        assert_eq!(restored.status, PlaybackStatus::Paused);
        assert_eq!(restored.current_time, 41.5);
        assert_eq!(restored.volume, 0.65);
        assert!(restored.shuffle_enabled);
        assert_eq!(restored.repeat_mode, super::RepeatMode::Loop);
        assert!(process.is_paused);
        assert_eq!(process.playhead_seconds, 41.5);
        assert_eq!(process.volume, 0.65);
    }

    #[test]
    fn player_crash_recovers_once_then_surfaces_an_actionable_error() {
        let (initial_event_sender, initial_events) = std::sync::mpsc::channel();
        let initial_process_state = Arc::new(Mutex::new(RestoredProcessState::default()));
        let initial_process = RestorableMpvProcess {
            state: initial_process_state,
        };
        let (recovery_event_sender, recovery_events) = std::sync::mpsc::channel();
        let recovery_events = Arc::new(Mutex::new(Some(recovery_events)));
        let recovered_process_state = Arc::new(Mutex::new(RestoredProcessState::default()));
        let starter_calls = Arc::new(Mutex::new(0_u8));
        let lifecycle = Arc::new(Mutex::new(PlaybackLifecycle::new()));
        let controller = PlaybackController::start_recoverable(
            Box::new(initial_process),
            initial_events,
            lifecycle,
            {
                let recovery_events = recovery_events.clone();
                let process_state = recovered_process_state.clone();
                let starter_calls = starter_calls.clone();
                move || {
                    *starter_calls.lock().expect("starter calls") += 1;
                    let events = recovery_events
                        .lock()
                        .expect("recovery events")
                        .take()
                        .ok_or_else(|| "recovery budget exhausted".to_owned())?;
                    Ok((
                        Box::new(RestorableMpvProcess {
                            state: process_state.clone(),
                        }) as Box<dyn MpvProcessAdapter>,
                        events,
                    ))
                }
            },
            |_| {},
        );
        let source = json!({
            "type": "track",
            "track": { "id": "track-1", "title": "Track 1", "durationMs": 120000 },
            "playbackUrl": "http://127.0.0.1:43129/token/api/v1/tracks/track-1/stream"
        });
        controller.play(Some(source.clone())).expect("play Track");
        controller.seek(27.5).expect("seek Track");

        initial_event_sender
            .send(MpvEvent::ExitedUnexpectedly("fixture crash".to_owned()))
            .expect("first crash");
        let recovered = wait_for_state(&controller, |state| {
            state.status == PlaybackStatus::Playing
                && state.current_time == 27.5
                && *starter_calls.lock().expect("starter calls") == 1
        });

        assert_eq!(recovered.source, Some(source));
        assert_eq!(*starter_calls.lock().expect("starter calls"), 1);
        assert!(
            !recovered_process_state
                .lock()
                .expect("recovered process")
                .is_paused
        );

        recovery_event_sender
            .send(MpvEvent::ExitedUnexpectedly(
                "fixture crash again".to_owned(),
            ))
            .expect("second crash");
        let failed = wait_for_state(&controller, |state| state.status == PlaybackStatus::Error);

        assert_eq!(*starter_calls.lock().expect("starter calls"), 1);
        assert!(
            failed
                .error
                .as_ref()
                .is_some_and(|error| error.message.contains("Quit and reopen the Desktop Client"))
        );
    }

    #[test]
    fn player_crash_without_a_restart_factory_publishes_an_actionable_error() {
        let (event_sender, events) = std::sync::mpsc::channel();
        let controller = PlaybackController::start(
            Box::new(RestorableMpvProcess {
                state: Arc::new(Mutex::new(RestoredProcessState::default())),
            }),
            events,
            |_| {},
        );
        controller
            .play(Some(json!({
                "type": "track",
                "track": { "id": "track-1", "title": "Track 1" },
                "playbackUrl": "http://127.0.0.1:43129/token/api/v1/tracks/track-1/stream"
            })))
            .expect("play Track");

        event_sender
            .send(MpvEvent::ExitedUnexpectedly("fixture crash".to_owned()))
            .expect("crash event");
        let failed = wait_for_state(&controller, |state| state.status == PlaybackStatus::Error);

        assert_eq!(
            failed.error.as_ref().map(|error| error.code.as_str()),
            Some("mpv-restart-unavailable")
        );
        assert!(
            failed
                .error
                .is_some_and(|error| error.message.contains("Restart the Desktop Client"))
        );
    }

    #[test]
    fn restart_internal_details_are_not_published_in_the_playback_session() {
        let (event_sender, events) = std::sync::mpsc::channel();
        let lifecycle = Arc::new(Mutex::new(PlaybackLifecycle::new()));
        let controller = PlaybackController::start_recoverable(
            Box::new(RestorableMpvProcess {
                state: Arc::new(Mutex::new(RestoredProcessState::default())),
            }),
            events,
            lifecycle,
            || Err("private IPC /tmp/secret.sock rejected token abc123".to_owned()),
            |_| {},
        );
        controller
            .play(Some(json!({
                "type": "track",
                "track": { "id": "track-1", "title": "Track 1" },
                "playbackUrl": "http://127.0.0.1:43129/token/api/v1/tracks/track-1/stream"
            })))
            .expect("play Track");

        event_sender
            .send(MpvEvent::ExitedUnexpectedly(
                "private child status".to_owned(),
            ))
            .expect("crash event");
        let failed = wait_for_state(&controller, |state| state.status == PlaybackStatus::Error);
        let error = failed.error.expect("actionable recovery error");

        assert_eq!(error.code, "mpv-restart-failed");
        assert_eq!(
            error.message,
            "Native playback could not restart. Quit and reopen the Desktop Client to try again."
        );
        assert!(!error.message.contains("secret"));
        assert!(!error.message.contains("token"));
    }

    #[test]
    fn poisoned_recovery_lifecycle_publishes_an_actionable_error() {
        let (event_sender, events) = std::sync::mpsc::channel();
        let lifecycle = Arc::new(Mutex::new(PlaybackLifecycle::new()));
        let controller = PlaybackController::start_recoverable(
            Box::new(RestorableMpvProcess {
                state: Arc::new(Mutex::new(RestoredProcessState::default())),
            }),
            events,
            lifecycle.clone(),
            || Err("restart must not be attempted".to_owned()),
            |_| {},
        );
        controller
            .play(Some(json!({
                "type": "track",
                "track": { "id": "track-1", "title": "Track 1" },
                "playbackUrl": "http://127.0.0.1:43129/token/api/v1/tracks/track-1/stream"
            })))
            .expect("play Track");
        let poisoned_lifecycle = lifecycle.clone();
        assert!(
            thread::spawn(move || {
                let _guard = poisoned_lifecycle.lock().expect("lifecycle");
                panic!("poison lifecycle fixture");
            })
            .join()
            .is_err()
        );

        event_sender
            .send(MpvEvent::ExitedUnexpectedly("fixture crash".to_owned()))
            .expect("crash event");
        let failed = wait_for_state(&controller, |state| state.status == PlaybackStatus::Error);

        assert_eq!(
            failed.error.as_ref().map(|error| error.code.as_str()),
            Some("mpv-recovery-lifecycle-unavailable")
        );
        assert!(
            failed
                .error
                .is_some_and(|error| error.message.contains("Restart the Desktop Client"))
        );
    }

    #[test]
    fn radio_and_catalog_sources_load_proxy_urls_as_continuous_streams() {
        let (_event_sender, event_receiver) = std::sync::mpsc::channel();
        let loaded_url = Arc::new(Mutex::new(None));
        let process = FakeMpvProcess {
            loaded_url: loaded_url.clone(),
            is_shutdown: Arc::new(AtomicBool::new(false)),
            load_error: None,
        };
        let controller = PlaybackController::start(Box::new(process), event_receiver, |_| {});
        let radio_source = json!({
            "type": "radio-station",
            "station": {
                "id": "station-1",
                "name": "Controlled MP3",
                "streamUrl": "https://radio.example/live.mp3"
            },
            "playbackUrl": "http://127.0.0.1:43129/token/api/v1/radio/stations/station-1/stream",
            "sourceUrl": "https://radio.example/live.mp3"
        });
        let catalog_source = json!({
            "type": "catalog-preview",
            "result": {
                "stationUuid": "catalog-1",
                "name": "Controlled HLS",
                "streamUrl": "https://catalog.example/live.m3u8"
            },
            "playbackUrl": "http://127.0.0.1:43129/token/api/v1/radio/preview/catalog-1/stream",
            "sourceUrl": "https://catalog.example/live.m3u8"
        });

        let radio = controller
            .play(Some(radio_source.clone()))
            .expect("play saved Radio Station");
        assert_eq!(radio.source, Some(radio_source));
        assert_eq!(radio.status, PlaybackStatus::Playing);
        assert_eq!(radio.duration, 0.0);
        assert_eq!(
            loaded_url.lock().expect("loaded URL").as_deref(),
            Some("http://127.0.0.1:43129/token/api/v1/radio/stations/station-1/stream")
        );

        let preview = controller
            .play(Some(catalog_source.clone()))
            .expect("play Catalog Preview");
        assert_eq!(preview.source, Some(catalog_source));
        assert_eq!(preview.status, PlaybackStatus::Playing);
        assert_eq!(preview.duration, 0.0);
        assert_eq!(
            loaded_url.lock().expect("loaded URL").as_deref(),
            Some("http://127.0.0.1:43129/token/api/v1/radio/preview/catalog-1/stream")
        );
    }

    #[test]
    fn controls_ended_and_errors_flow_through_native_state() {
        let (event_sender, event_receiver) = std::sync::mpsc::channel();
        let is_shutdown = Arc::new(AtomicBool::new(false));
        let process = FakeMpvProcess {
            loaded_url: Arc::new(Mutex::new(None)),
            is_shutdown: is_shutdown.clone(),
            load_error: None,
        };
        let controller = PlaybackController::start(Box::new(process), event_receiver, |_| {});
        let source = json!({
            "type": "track",
            "track": { "id": "track-1", "title": "Track 1", "durationMs": 120000 },
            "playbackUrl": "http://127.0.0.1:43129/token/api/v1/tracks/track-1/stream"
        });

        controller.play(Some(source)).expect("play Track");
        controller.pause().expect("pause Track");
        controller.seek(25.0).expect("seek Track");
        event_sender.send(MpvEvent::Ended).expect("ended event");
        let ended = wait_for_state(&controller, |state| state.status == PlaybackStatus::Ended);
        event_sender
            .send(MpvEvent::Error("decoder failed".to_owned()))
            .expect("error event");
        let failed = wait_for_state(&controller, |state| state.status == PlaybackStatus::Error);

        assert_eq!(ended.current_time, 25.0);
        assert_eq!(
            failed.error.as_ref().map(|error| error.message.as_str()),
            Some("Playback failed. Check the source and try again.")
        );
        drop(controller);
        assert!(is_shutdown.load(Ordering::Acquire));
    }

    #[test]
    fn native_queue_context_owns_previous_and_next_selection() {
        let (_event_sender, event_receiver) = std::sync::mpsc::channel();
        let loaded_url = Arc::new(Mutex::new(None));
        let controller = PlaybackController::start(
            Box::new(FakeMpvProcess {
                loaded_url: loaded_url.clone(),
                is_shutdown: Arc::new(AtomicBool::new(false)),
                load_error: None,
            }),
            event_receiver,
            |_| {},
        );
        let sources = queue_sources();
        controller
            .sync_queue_context(sources.clone(), Some(1))
            .expect("sync Queue context");
        controller
            .play(Some(sources[1].clone()))
            .expect("play middle Track");

        let previous = controller.previous().expect("previous Track");
        let next = controller.next().expect("next Track");

        assert_eq!(previous.source, Some(sources[0].clone()));
        assert_eq!(next.source, Some(sources[1].clone()));
        assert_eq!(
            loaded_url.lock().expect("loaded URL").as_deref(),
            Some("http://127.0.0.1/token/api/v1/tracks/track-2/stream")
        );
    }

    #[test]
    fn native_shuffle_and_end_advancement_use_the_synced_queue_without_mutating_it() {
        let (event_sender, event_receiver) = std::sync::mpsc::channel();
        let controller = PlaybackController::start(
            Box::new(FakeMpvProcess {
                loaded_url: Arc::new(Mutex::new(None)),
                is_shutdown: Arc::new(AtomicBool::new(false)),
                load_error: None,
            }),
            event_receiver,
            |_| {},
        );
        let sources = queue_sources();
        controller
            .sync_queue_context(sources.clone(), Some(0))
            .expect("sync Queue context");
        controller
            .play(Some(sources[0].clone()))
            .expect("play first Track");
        controller.toggle_shuffle().expect("enable shuffle");

        event_sender.send(MpvEvent::Ended).expect("ended event");
        let advanced = wait_for_state(&controller, |state| {
            state.source.as_ref() != Some(&sources[0])
        });

        assert_ne!(advanced.source, Some(sources[1].clone()));
        assert!(sources.contains(advanced.source.as_ref().expect("advanced source")));
        assert_eq!(controller.queue_sources().expect("Queue sources"), sources);
    }

    #[test]
    fn process_failures_are_published_as_playback_errors() {
        let (_event_sender, event_receiver) = std::sync::mpsc::channel();
        let process = FakeMpvProcess {
            loaded_url: Arc::new(Mutex::new(None)),
            is_shutdown: Arc::new(AtomicBool::new(false)),
            load_error: Some("fixture load failed".to_owned()),
        };
        let controller = PlaybackController::start(Box::new(process), event_receiver, |_| {});
        let source = json!({
            "type": "track",
            "track": { "id": "track-1", "title": "Track 1", "durationMs": 120000 },
            "playbackUrl": "http://127.0.0.1:43129/token/api/v1/tracks/track-1/stream"
        });

        let error = controller.play(Some(source)).expect_err("load must fail");
        let state = controller.state().expect("playback state");

        assert_eq!(
            error.message,
            "Playback failed. Check the source and try again."
        );
        assert_eq!(state.status, PlaybackStatus::Error);
        assert_eq!(
            state.error.as_ref().map(|error| error.message.as_str()),
            Some("Playback failed. Check the source and try again.")
        );
    }

    fn wait_for_state(
        controller: &PlaybackController,
        predicate: impl Fn(&super::PlaybackSessionState) -> bool,
    ) -> super::PlaybackSessionState {
        let deadline = Instant::now() + Duration::from_secs(1);
        loop {
            let state = controller.state().expect("playback state");
            if predicate(&state) {
                return state;
            }
            assert!(Instant::now() < deadline, "timed out waiting for state");
            thread::sleep(Duration::from_millis(5));
        }
    }

    fn queue_sources() -> Vec<serde_json::Value> {
        (1..=3)
            .map(|index| {
                json!({
                    "type": "track",
                    "track": {
                        "id": format!("track-{index}"),
                        "title": format!("Track {index}"),
                        "durationMs": 120000
                    },
                    "playbackUrl": format!(
                        "http://127.0.0.1/token/api/v1/tracks/track-{index}/stream"
                    )
                })
            })
            .collect()
    }

    #[cfg(target_os = "linux")]
    #[test]
    #[ignore = "requires the pinned Linux mpv executable"]
    fn real_pinned_mpv_controls_a_track_and_removes_private_ipc() {
        let fixture_path = temporary_path("controlled-track.wav");
        fs::write(&fixture_path, silent_wav()).expect("write WAV fixture");
        let (process, events, ipc_directory) =
            RealMpvProcess::start(super::resolve_mpv_binary(), vec!["--ao=null".to_owned()])
                .expect("start pinned mpv");
        assert!(ipc_directory.exists());
        assert_eq!(
            fs::metadata(&ipc_directory)
                .expect("private IPC directory")
                .permissions()
                .mode()
                & 0o777,
            0o700
        );
        assert_eq!(
            fs::metadata(ipc_directory.join("control.sock"))
                .expect("private IPC socket")
                .permissions()
                .mode()
                & 0o777,
            0o600
        );
        let controller = PlaybackController::start(process, events, |_| {});
        let mut processed = ProcessingState::default();
        processed.profile = crate::processing::ProcessingProfile::Processed;
        processed.software_volume = 0.37;
        processed.replay_gain_mode = ReplayGainMode::Album;
        processed.equalizer.is_enabled = true;
        let applied = controller
            .apply_processing(
                processed,
                &MpvProcessingConfiguration {
                    volume_percent: 37.0,
                    replay_gain_mode: ReplayGainMode::Album,
                    audio_filters: vec!["equalizer=f=1000:t=q:w=1:g=2".to_owned()],
                },
            )
            .expect("observe effective mpv processing");
        let source = json!({
            "type": "track",
            "track": { "id": "fixture", "title": "Fixture", "durationMs": 1000 },
            "playbackUrl": fixture_path.to_string_lossy()
        });

        controller.play(Some(source)).expect("play fixture");
        let playing = wait_for_state(&controller, |state| state.current_time > 0.0);
        controller.pause().expect("pause fixture");
        controller.seek(0.5).expect("seek fixture");

        assert_eq!(applied.processing.software_volume, 0.37);
        assert_eq!(applied.processing.replay_gain_mode, ReplayGainMode::Album);
        assert!(applied.processing.equalizer.is_enabled);
        assert!(!applied.processing.effective_audio_filters.is_empty());
        assert_eq!(playing.status, PlaybackStatus::Playing);
        assert!(playing.duration > 0.9);
        drop(controller);
        assert!(!ipc_directory.exists());
        fs::remove_file(fixture_path).expect("remove WAV fixture");
    }

    #[cfg(target_os = "linux")]
    #[test]
    #[ignore = "requires the pinned Linux mpv executable and desktop audio lifecycle"]
    fn linux_background_playback_continues_until_clean_explicit_quit() {
        let fixture_path = temporary_path("background-track.wav");
        let snapshot_path = temporary_path("playback-session.json");
        fs::write(&fixture_path, silent_wav()).expect("write WAV fixture");
        let (process, events, ipc_directory) =
            RealMpvProcess::start(super::resolve_mpv_binary(), vec!["--ao=null".to_owned()])
                .expect("start pinned mpv");
        let controller = PlaybackController::start(process, events, |_| {});
        let source = json!({
            "type": "track",
            "track": { "id": "fixture", "title": "Fixture", "durationMs": 1000 },
            "playbackUrl": fixture_path.to_string_lossy()
        });
        controller.play(Some(source)).expect("play fixture");
        let before_close = wait_for_state(&controller, |state| state.current_time > 0.0);
        let lifecycle = Arc::new(Mutex::new(PlaybackLifecycle::new()));
        let snapshot_store = PlaybackSnapshotStore::new(snapshot_path.clone());
        let shell = TestDesktopPlaybackShell::default();

        dispatch_desktop_playback_action(
            DesktopPlaybackAction::CloseMainWindow,
            &controller,
            &lifecycle,
            &snapshot_store,
            &shell,
        )
        .expect("dispatch actual close action");
        let background = wait_for_state(&controller, |state| {
            state.current_time > before_close.current_time
        });
        dispatch_desktop_playback_action(
            DesktopPlaybackAction::Quit,
            &controller,
            &lifecycle,
            &snapshot_store,
            &shell,
        )
        .expect("dispatch actual tray Quit action");
        drop(controller);

        assert!(shell.is_hidden.load(Ordering::Acquire));
        assert!(shell.has_exited.load(Ordering::Acquire));
        assert!(background.current_time > before_close.current_time);
        assert!(snapshot_path.exists());
        assert!(!ipc_directory.exists());
        fs::remove_file(fixture_path).expect("remove WAV fixture");
        fs::remove_file(snapshot_path).expect("remove snapshot fixture");
    }

    #[cfg(target_os = "linux")]
    #[test]
    #[ignore = "requires pinned Linux mpv and ffmpeg fixture generation"]
    fn real_pinned_mpv_streams_controlled_mp3_icy_and_aac_hls_sources() {
        let fixture_directory = temporary_path("controlled-streams");
        fs::create_dir(&fixture_directory).expect("create stream fixture directory");
        let mp3 = generate_audio_fixture(&fixture_directory, "radio.mp3", "libmp3lame");
        let aac = generate_audio_fixture(&fixture_directory, "segment.aac", "aac");
        let server = ControlledStreamServer::start(mp3, aac);
        let (process, events, _) =
            RealMpvProcess::start(super::resolve_mpv_binary(), vec!["--ao=null".to_owned()])
                .expect("start pinned mpv");
        let controller = PlaybackController::start(process, events, |_| {});

        play_controlled_stream(
            &controller,
            "radio-station",
            format!("{}/radio.mp3", server.base_url),
        );
        play_controlled_stream(
            &controller,
            "catalog-preview",
            format!("{}/catalog.m3u8", server.base_url),
        );

        drop(controller);
        server.shutdown();
        fs::remove_dir_all(fixture_directory).expect("remove stream fixture directory");
    }

    fn play_controlled_stream(
        controller: &PlaybackController,
        source_type: &str,
        playback_url: String,
    ) {
        let source = match source_type {
            "radio-station" => json!({
                "type": source_type,
                "station": { "id": "controlled-radio", "name": "Controlled ICY MP3" },
                "playbackUrl": playback_url
            }),
            _ => json!({
                "type": source_type,
                "result": { "stationUuid": "controlled-catalog", "name": "Controlled AAC HLS" },
                "playbackUrl": playback_url
            }),
        };
        controller
            .play(Some(source))
            .expect("play controlled stream");
        wait_for_state(controller, |state| state.current_time > 0.05);
    }

    #[test]
    fn mpv_uses_only_application_owned_audio_configuration() {
        let mut command = Command::new("mpv");
        let socket_path = PathBuf::from("/private/control.sock");

        super::configure_mpv_command(&mut command, &socket_path, Vec::new());

        let arguments = command
            .get_args()
            .map(|argument| argument.to_string_lossy().into_owned())
            .collect::<Vec<_>>();
        assert!(arguments.contains(&"--no-config".to_owned()));
        assert!(arguments.contains(&"--idle=yes".to_owned()));
        assert!(arguments.contains(&"--no-video".to_owned()));
        assert!(arguments.contains(&"--input-terminal=no".to_owned()));
        assert!(arguments.contains(&"--gapless-audio=weak".to_owned()));
        assert!(arguments.contains(&"--input-ipc-server=/private/control.sock".to_owned()));
    }

    #[test]
    fn desktop_config_packages_the_pinned_mpv_sidecar() {
        let config: serde_json::Value =
            serde_json::from_str(include_str!("../tauri.sidecar.conf.json"))
                .expect("Tauri sidecar config");

        assert_eq!(
            config.pointer("/bundle/externalBin"),
            Some(&json!(["binaries/mpv"]))
        );
    }

    fn temporary_path(file_name: &str) -> PathBuf {
        let mut random = [0_u8; 8];
        getrandom::fill(&mut random).expect("random temporary name");
        std::env::temp_dir().join(format!(
            "earthly-audio-test-{}-{file_name}",
            u64::from_le_bytes(random)
        ))
    }

    fn silent_wav() -> Vec<u8> {
        const SAMPLE_RATE: u32 = 8_000;
        const DATA_SIZE: u32 = SAMPLE_RATE * 2;
        let mut wav = Vec::with_capacity(44 + DATA_SIZE as usize);
        wav.extend_from_slice(b"RIFF");
        wav.extend_from_slice(&(36 + DATA_SIZE).to_le_bytes());
        wav.extend_from_slice(b"WAVEfmt \x10\0\0\0\x01\0\x01\0");
        wav.extend_from_slice(&SAMPLE_RATE.to_le_bytes());
        wav.extend_from_slice(&(SAMPLE_RATE * 2).to_le_bytes());
        wav.extend_from_slice(&2_u16.to_le_bytes());
        wav.extend_from_slice(&16_u16.to_le_bytes());
        wav.extend_from_slice(b"data");
        wav.extend_from_slice(&DATA_SIZE.to_le_bytes());
        wav.resize(44 + DATA_SIZE as usize, 0);
        wav
    }
}

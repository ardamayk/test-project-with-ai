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
    pub(crate) status: PlaybackStatus,
    pub(crate) current_time: f64,
    pub(crate) duration: f64,
    volume: f64,
    shuffle_enabled: bool,
    repeat_mode: RepeatMode,
    error: Option<PlaybackStateError>,
}

impl Default for PlaybackSessionState {
    fn default() -> Self {
        Self {
            source: None,
            status: PlaybackStatus::Idle,
            current_time: 0.0,
            duration: 0.0,
            volume: DEFAULT_VOLUME,
            shuffle_enabled: false,
            repeat_mode: RepeatMode::Off,
            error: None,
        }
    }
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct PlaybackCommandError {
    code: &'static str,
    message: String,
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
    Ended,
    Error(String),
}

pub(crate) trait MpvProcessAdapter: Send + Sync {
    fn load(&self, url: &str) -> Result<(), String>;
    fn set_paused(&self, is_paused: bool) -> Result<(), String>;
    fn seek(&self, seconds: f64) -> Result<(), String>;
    fn set_volume(&self, value: f64) -> Result<(), String>;
    fn stop(&self) -> Result<(), String>;
    fn shutdown(&self);
}

enum MpvWorkerRequest {
    Command {
        command: Value,
        response: Sender<Result<(), String>>,
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
                let _ = fs::remove_dir_all(&ipc_directory);
                Err(error)
            }
        }
    }

    fn observe_properties(&self) -> Result<(), String> {
        self.command(json!(["observe_property", 1, "time-pos"]))?;
        self.command(json!(["observe_property", 2, "duration"]))?;
        self.command(json!(["observe_property", 3, "pause"]))
    }

    fn command(&self, command: Value) -> Result<(), String> {
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
        let _ = self.requests.send(MpvWorkerRequest::Shutdown);
        if let Ok(mut worker) = self.worker.lock()
            && let Some(worker) = worker.take()
        {
            let _ = worker.join();
        }
        let _ = fs::remove_dir_all(&self.ipc_directory);
    }
}

fn resolve_mpv_binary() -> PathBuf {
    if let Some(binary) = std::env::var_os("EARTHLY_AUDIO_MPV_PATH") {
        return PathBuf::from(binary);
    }
    if let Ok(executable) = std::env::current_exe()
        && let Some(directory) = executable.parent()
    {
        let packaged_binary = directory.join("mpv");
        if packaged_binary.is_file() {
            return packaged_binary;
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
    }

    fn set_paused(&self, is_paused: bool) -> Result<(), String> {
        self.command(json!(["set_property", "pause", is_paused]))
    }

    fn seek(&self, seconds: f64) -> Result<(), String> {
        self.command(json!(["seek", seconds, "absolute+exact"]))
    }

    fn set_volume(&self, value: f64) -> Result<(), String> {
        self.command(json!(["set_property", "volume", value * 100.0]))
    }

    fn stop(&self) -> Result<(), String> {
        self.command(json!(["stop"]))
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
    let _ = child.kill();
    let _ = child.wait();
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
            let _ = events.send(MpvEvent::Error(format!("Pinned mpv IPC failed: {error}")));
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
                let _ = response.send(result);
            }
            Ok(MpvWorkerRequest::Shutdown) => break,
            Err(RecvTimeoutError::Timeout) => drain_mpv_event(&mut reader, &events),
            Err(RecvTimeoutError::Disconnected) => break,
        }
        match child.try_wait() {
            Ok(Some(status)) => {
                let _ = events.send(MpvEvent::Error(format!(
                    "Pinned mpv exited unexpectedly with {status}."
                )));
                return;
            }
            Ok(None) => {}
            Err(error) => {
                let _ = events.send(MpvEvent::Error(format!(
                    "Pinned mpv status could not be read: {error}"
                )));
                break;
            }
        }
    }
    let _ = write_mpv_request(&mut stream, 0, json!(["quit"]));
    terminate_mpv(&mut child);
}

fn execute_mpv_command(
    stream: &mut UnixStream,
    reader: &mut BufReader<UnixStream>,
    events: &Sender<MpvEvent>,
    request_id: u64,
    command: Value,
) -> Result<(), String> {
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
) -> Result<Option<Result<(), String>>, String> {
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

fn mpv_response_result(message: &Value) -> Result<(), String> {
    match message.get("error").and_then(Value::as_str) {
        Some("success") => Ok(()),
        Some(error) => Err(format!("Pinned mpv rejected command: {error}")),
        None => Err("Pinned mpv returned a command response without status.".to_owned()),
    }
}

fn drain_mpv_event(reader: &mut BufReader<UnixStream>, events: &Sender<MpvEvent>) {
    let _ = read_mpv_message(reader, events, u64::MAX);
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
        let _ = events.send(event);
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
        _ => None,
    }
}

fn terminate_mpv(child: &mut Child) {
    let deadline = Instant::now() + MPV_SHUTDOWN_TIMEOUT;
    while Instant::now() < deadline {
        if child.try_wait().ok().flatten().is_some() {
            return;
        }
        thread::sleep(Duration::from_millis(10));
    }
    let _ = child.kill();
    let _ = child.wait();
}

type StateListener = Arc<dyn Fn(PlaybackSessionState) + Send + Sync>;

pub(crate) struct PlaybackController {
    process: Arc<Mutex<Box<dyn MpvProcessAdapter>>>,
    state: Arc<Mutex<PlaybackSessionState>>,
    is_shutdown: Arc<AtomicBool>,
    event_thread: Option<JoinHandle<()>>,
    listener: StateListener,
}

impl PlaybackController {
    pub(crate) fn start_default(
        listener: impl Fn(PlaybackSessionState) + Send + Sync + 'static,
    ) -> Result<Self, String> {
        let (process, events) = RealMpvProcess::start_default()?;
        Ok(Self::start(process, events, listener))
    }

    pub(crate) fn start(
        process: Box<dyn MpvProcessAdapter>,
        events: Receiver<MpvEvent>,
        listener: impl Fn(PlaybackSessionState) + Send + Sync + 'static,
    ) -> Self {
        let process = Arc::new(Mutex::new(process));
        let state = Arc::new(Mutex::new(PlaybackSessionState::default()));
        let is_shutdown = Arc::new(AtomicBool::new(false));
        let listener: StateListener = Arc::new(listener);
        let event_thread = spawn_event_thread(
            process.clone(),
            state.clone(),
            events,
            is_shutdown.clone(),
            listener.clone(),
        );
        Self {
            process,
            state,
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

    pub(crate) fn play(
        &self,
        source: Option<Value>,
    ) -> Result<PlaybackSessionState, PlaybackCommandError> {
        let current = self.state()?;
        let next_source = source.clone().or(current.source.clone());
        let Some(next_source) = next_source else {
            return Ok(current);
        };
        let action = if source.is_some() {
            let url = playback_url(&next_source)?;
            self.with_process(|process| {
                process.load(url)?;
                process.set_paused(false)
            })
        } else {
            self.with_process(|process| process.set_paused(false))
        };
        if let Err(error) = action {
            return self.fail(error);
        }
        self.update(|state| {
            state.source = Some(next_source.clone());
            state.status = PlaybackStatus::Playing;
            state.current_time = if source.is_some() {
                0.0
            } else {
                state.current_time
            };
            if source.is_some() {
                state.duration = track_duration(&next_source);
            }
            state.error = None;
        })
    }

    pub(crate) fn pause(&self) -> Result<PlaybackSessionState, PlaybackCommandError> {
        if self.state()?.source.is_none() {
            return self.state();
        }
        if let Err(error) = self.with_process(|process| process.set_paused(true)) {
            return self.fail(error);
        }
        self.update(|state| state.status = PlaybackStatus::Paused)
    }

    pub(crate) fn stop(&self) -> Result<PlaybackSessionState, PlaybackCommandError> {
        if let Err(error) = self.with_process(|process| process.stop()) {
            return self.fail(error);
        }
        self.update(|state| {
            state.source = None;
            state.status = PlaybackStatus::Idle;
            state.current_time = 0.0;
            state.duration = 0.0;
            state.error = None;
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
            return self.fail(error);
        }
        self.update(|state| state.current_time = seconds)
    }

    pub(crate) fn set_volume(
        &self,
        value: f64,
    ) -> Result<PlaybackSessionState, PlaybackCommandError> {
        if !value.is_finite() {
            return self.fail("Playback volume must be a finite number.");
        }
        let volume = value.clamp(0.0, 1.0);
        if let Err(error) = self.with_process(|process| process.set_volume(volume)) {
            return self.fail(error);
        }
        self.update(|state| state.volume = volume)
    }

    pub(crate) fn toggle_shuffle(&self) -> Result<PlaybackSessionState, PlaybackCommandError> {
        self.update(|state| state.shuffle_enabled = !state.shuffle_enabled)
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

    fn fail(
        &self,
        message: impl Into<String>,
    ) -> Result<PlaybackSessionState, PlaybackCommandError> {
        let message = message.into();
        let _ = self.update(|state| set_error_state(state, message.clone()));
        Err(PlaybackCommandError::new(message))
    }
}

impl Drop for PlaybackController {
    fn drop(&mut self) {
        self.is_shutdown.store(true, Ordering::Release);
        if let Ok(process) = self.process.lock() {
            process.shutdown();
        }
        if let Some(event_thread) = self.event_thread.take() {
            let _ = event_thread.join();
        }
    }
}

fn spawn_event_thread(
    process: Arc<Mutex<Box<dyn MpvProcessAdapter>>>,
    state: Arc<Mutex<PlaybackSessionState>>,
    events: Receiver<MpvEvent>,
    is_shutdown: Arc<AtomicBool>,
    listener: StateListener,
) -> JoinHandle<()> {
    thread::spawn(move || {
        while !is_shutdown.load(Ordering::Acquire) {
            match events.recv_timeout(EVENT_POLL_INTERVAL) {
                Ok(event) => handle_mpv_event(&process, &state, event, &listener),
                Err(RecvTimeoutError::Timeout) => {}
                Err(RecvTimeoutError::Disconnected) => break,
            }
        }
    })
}

fn handle_mpv_event(
    process: &Arc<Mutex<Box<dyn MpvProcessAdapter>>>,
    state: &Arc<Mutex<PlaybackSessionState>>,
    event: MpvEvent,
    listener: &StateListener,
) {
    let result = match event {
        MpvEvent::Time(value) if value.is_finite() && value >= 0.0 => {
            update_shared_state(state, |state| state.current_time = value)
        }
        MpvEvent::Duration(value) if value.is_finite() && value > 0.0 => {
            update_shared_state(state, |state| state.duration = value)
        }
        MpvEvent::Paused(is_paused) => update_shared_state(state, |state| {
            if state.source.is_some() && state.status != PlaybackStatus::Ended {
                state.status = if is_paused {
                    PlaybackStatus::Paused
                } else {
                    PlaybackStatus::Playing
                };
            }
        }),
        MpvEvent::Ended => handle_ended_event(process, state),
        MpvEvent::Error(message) => {
            update_shared_state(state, |state| set_error_state(state, message.clone()))
        }
        MpvEvent::Time(_) | MpvEvent::Duration(_) => return,
    };
    if let Ok(next) = result {
        listener(next);
    }
}

fn handle_ended_event(
    process: &Arc<Mutex<Box<dyn MpvProcessAdapter>>>,
    state: &Arc<Mutex<PlaybackSessionState>>,
) -> Result<PlaybackSessionState, PlaybackCommandError> {
    let should_repeat = state
        .lock()
        .map_err(|_| PlaybackCommandError::new("Native playback state is unavailable."))?
        .repeat_mode
        != RepeatMode::Off;
    if should_repeat {
        let repeat_result = process
            .lock()
            .map_err(|_| "Native mpv process is unavailable.".to_owned())
            .and_then(|process| {
                process.seek(0.0)?;
                process.set_paused(false)
            });
        if let Err(message) = repeat_result {
            return update_shared_state(state, |state| set_error_state(state, message.clone()));
        }
    }
    update_shared_state(state, |state| {
        state.current_time = if should_repeat {
            0.0
        } else {
            state.current_time
        };
        state.status = if should_repeat {
            PlaybackStatus::Playing
        } else {
            PlaybackStatus::Ended
        };
        if state.repeat_mode == RepeatMode::Once {
            state.repeat_mode = RepeatMode::Off;
        }
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

fn playback_url(source: &Value) -> Result<&str, PlaybackCommandError> {
    source
        .get("playbackUrl")
        .and_then(Value::as_str)
        .filter(|url| !url.is_empty())
        .ok_or_else(|| PlaybackCommandError::new("Playback Source URL is missing."))
}

fn track_duration(source: &Value) -> f64 {
    if source.get("type").and_then(Value::as_str) != Some("track") {
        return 0.0;
    }
    source
        .pointer("/track/durationMs")
        .and_then(Value::as_f64)
        .filter(|duration| duration.is_finite() && *duration > 0.0)
        .map_or(0.0, |duration| duration / 1000.0)
}

#[cfg(test)]
mod tests {
    use super::{MpvEvent, MpvProcessAdapter, PlaybackController, PlaybackStatus, RealMpvProcess};
    use serde_json::json;
    use std::fs;
    use std::os::unix::fs::PermissionsExt;
    use std::path::PathBuf;
    use std::process::Command;
    use std::sync::atomic::{AtomicBool, Ordering};
    use std::sync::{Arc, Mutex};
    use std::thread;
    use std::time::{Duration, Instant};

    struct FakeMpvProcess {
        loaded_url: Arc<Mutex<Option<String>>>,
        is_shutdown: Arc<AtomicBool>,
        load_error: Option<String>,
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
            Some("decoder failed")
        );
        drop(controller);
        assert!(is_shutdown.load(Ordering::Acquire));
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

        assert_eq!(error.message, "fixture load failed");
        assert_eq!(state.status, PlaybackStatus::Error);
        assert_eq!(
            state.error.as_ref().map(|error| error.message.as_str()),
            Some("fixture load failed")
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

    #[cfg(target_os = "linux")]
    #[test]
    #[ignore = "requires the pinned Linux mpv executable"]
    fn real_pinned_mpv_controls_a_track_and_removes_private_ipc() {
        let fixture_path = temporary_path("controlled-track.wav");
        fs::write(&fixture_path, silent_wav()).expect("write WAV fixture");
        let (process, events, ipc_directory) =
            RealMpvProcess::start(PathBuf::from("/usr/bin/mpv"), vec!["--ao=null".to_owned()])
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
        let source = json!({
            "type": "track",
            "track": { "id": "fixture", "title": "Fixture", "durationMs": 1000 },
            "playbackUrl": fixture_path.to_string_lossy()
        });

        controller.play(Some(source)).expect("play fixture");
        let playing = wait_for_state(&controller, |state| state.current_time > 0.0);
        controller.pause().expect("pause fixture");
        controller.seek(0.5).expect("seek fixture");

        assert_eq!(playing.status, PlaybackStatus::Playing);
        assert!(playing.duration > 0.9);
        drop(controller);
        assert!(!ipc_directory.exists());
        fs::remove_file(fixture_path).expect("remove WAV fixture");
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

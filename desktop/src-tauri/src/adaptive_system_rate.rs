use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::path::PathBuf;
use std::process::Command;
use std::sync::Arc;

pub const ADAPTIVE_SYSTEM_RATE_WARNING: &str =
    "Adaptive System Rate affects audio from every application on the PipeWire graph.";

pub trait PipeWireRateAdapter: Send + Sync {
    fn forced_rate_hz(&self) -> Result<Option<u32>, String>;
    fn set_forced_rate_hz(&self, rate_hz: Option<u32>) -> Result<(), String>;
}

pub struct CommandPipeWireRateAdapter {
    metadata_binary: PathBuf,
    dump_binary: PathBuf,
}

impl CommandPipeWireRateAdapter {
    pub fn new() -> Self {
        Self::with_binaries(PathBuf::from("pw-metadata"), PathBuf::from("pw-dump"))
    }

    pub fn with_binary(binary: PathBuf) -> Self {
        Self::with_binaries(binary.clone(), binary)
    }

    pub fn with_binaries(metadata_binary: PathBuf, dump_binary: PathBuf) -> Self {
        Self {
            metadata_binary,
            dump_binary,
        }
    }

    fn metadata_command(&self) -> Command {
        let mut command = Command::new(&self.metadata_binary);
        command.args(["-n", "settings", "0", "clock.force-rate"]);
        command
    }
}

impl Default for CommandPipeWireRateAdapter {
    fn default() -> Self {
        Self::new()
    }
}

impl PipeWireRateAdapter for CommandPipeWireRateAdapter {
    fn forced_rate_hz(&self) -> Result<Option<u32>, String> {
        let output = match Command::new(&self.dump_binary).arg("-N").output() {
            Ok(output) => output,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(None),
            Err(error) => return Err(self.command_error("query", &error.to_string(), true)),
        };
        if !output.status.success() {
            return Err(self.status_error("query", &output, true));
        }
        parse_force_rate(&output.stdout)
    }

    fn set_forced_rate_hz(&self, rate_hz: Option<u32>) -> Result<(), String> {
        let rate = rate_hz.unwrap_or(0).to_string();
        let output = self
            .metadata_command()
            .arg(&rate)
            .output()
            .map_err(|error| self.command_error("set", &error.to_string(), false))?;
        if !output.status.success() {
            return Err(self.status_error("set", &output, false));
        }
        Ok(())
    }
}

impl CommandPipeWireRateAdapter {
    fn command_error(&self, operation: &str, detail: &str, is_query: bool) -> String {
        let binary = if is_query {
            &self.dump_binary
        } else {
            &self.metadata_binary
        };
        format!(
            "Failed to {operation} PipeWire clock.force-rate with {}: {detail}",
            binary.display()
        )
    }

    fn status_error(
        &self,
        operation: &str,
        output: &std::process::Output,
        is_query: bool,
    ) -> String {
        let status = output
            .status
            .code()
            .map(|code| code.to_string())
            .unwrap_or_else(|| "terminated by signal".to_owned());
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_owned();
        self.command_error(
            operation,
            &format!("exited with status {status}: {stderr}"),
            is_query,
        )
    }
}

fn parse_force_rate(output: &[u8]) -> Result<Option<u32>, String> {
    let objects: Vec<Value> = serde_json::from_slice(output)
        .map_err(|error| format!("Failed to parse PipeWire metadata: {error}"))?;
    let Some(settings) = objects.iter().find(|object| {
        object
            .pointer("/props/metadata.name")
            .and_then(Value::as_str)
            == Some("settings")
    }) else {
        return Ok(None);
    };
    let Some(value) = settings
        .get("metadata")
        .and_then(Value::as_array)
        .and_then(|metadata| {
            metadata.iter().find_map(|entry| {
                (entry.get("key").and_then(Value::as_str) == Some("clock.force-rate"))
                    .then(|| entry.get("value"))
                    .flatten()
            })
        })
    else {
        return Ok(None);
    };
    let rate_hz = value
        .as_u64()
        .and_then(|rate| u32::try_from(rate).ok())
        .or_else(|| Value::as_str(value).and_then(|rate| rate.parse().ok()))
        .ok_or_else(|| "PipeWire clock.force-rate metadata was invalid.".to_owned())?;
    Ok((rate_hz > 0).then_some(rate_hz))
}

#[derive(Clone, Debug, Default, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct AdaptiveSystemRateState {
    pub is_enabled: bool,
    pub forced_rate_hz: Option<u32>,
}

pub struct AdaptiveSystemRateController {
    adapter: Arc<dyn PipeWireRateAdapter>,
    state: AdaptiveSystemRateState,
}

impl AdaptiveSystemRateController {
    pub fn new(adapter: Arc<dyn PipeWireRateAdapter>) -> Self {
        Self {
            adapter,
            state: AdaptiveSystemRateState::default(),
        }
    }

    pub fn recover_startup(adapter: Arc<dyn PipeWireRateAdapter>) -> Result<Self, String> {
        if adapter.forced_rate_hz()?.is_some() {
            adapter.set_forced_rate_hz(None)?;
        }
        Ok(Self::new(adapter))
    }

    pub fn state(&self) -> &AdaptiveSystemRateState {
        &self.state
    }

    pub fn enable(&mut self, is_confirmed: bool) -> Result<(), String> {
        if !is_confirmed {
            return Err(ADAPTIVE_SYSTEM_RATE_WARNING.to_owned());
        }
        self.state.is_enabled = true;
        Ok(())
    }

    pub fn apply_source_sample_rate(&mut self, rate_hz: Option<u32>) -> Result<(), String> {
        if !self.state.is_enabled {
            return Ok(());
        }
        let rate_hz = rate_hz.filter(|rate| *rate > 0);
        self.adapter.set_forced_rate_hz(rate_hz)?;
        self.state.forced_rate_hz = rate_hz;
        Ok(())
    }

    pub fn disable(&mut self) -> Result<(), String> {
        if !self.state.is_enabled && self.state.forced_rate_hz.is_none() {
            return Ok(());
        }
        self.reset_force_rate()?;
        self.state = AdaptiveSystemRateState::default();
        Ok(())
    }

    pub fn reset_for_recovery(&mut self) -> Result<(), String> {
        if !self.state.is_enabled && self.state.forced_rate_hz.is_none() {
            return Ok(());
        }
        self.reset_force_rate()
    }

    fn reset_force_rate(&mut self) -> Result<(), String> {
        self.adapter.set_forced_rate_hz(None)?;
        self.state.forced_rate_hz = None;
        Ok(())
    }
}

impl Drop for AdaptiveSystemRateController {
    fn drop(&mut self) {
        if (self.state.is_enabled || self.state.forced_rate_hz.is_some())
            && let Err(error) = self.adapter.set_forced_rate_hz(None)
        {
            eprintln!("Adaptive System Rate teardown failed: {error}");
        }
    }
}

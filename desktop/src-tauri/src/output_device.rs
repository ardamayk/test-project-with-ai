use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use std::process::Command;

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct OutputDevice {
    pub id: String,
    pub name: String,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum OutputDeviceError {
    EnumerationFailed(String),
    NotRawHardware,
    SelectedDeviceDisconnected(OutputDevice),
    Unavailable,
}

pub trait AlsaOutputDeviceAdapter: Send + Sync {
    fn list_raw_devices(&self) -> Result<Vec<OutputDevice>, String>;
}

pub struct CommandAlsaOutputDeviceAdapter {
    binary: PathBuf,
}

impl CommandAlsaOutputDeviceAdapter {
    pub fn new() -> Self {
        Self::with_binary(PathBuf::from("aplay"))
    }

    pub fn with_binary(binary: PathBuf) -> Self {
        Self { binary }
    }
}

impl Default for CommandAlsaOutputDeviceAdapter {
    fn default() -> Self {
        Self::new()
    }
}

impl AlsaOutputDeviceAdapter for CommandAlsaOutputDeviceAdapter {
    fn list_raw_devices(&self) -> Result<Vec<OutputDevice>, String> {
        let output = Command::new(&self.binary)
            .arg("-l")
            .output()
            .map_err(|error| format!("Failed to enumerate raw ALSA hardware: {error}"))?;
        if !output.status.success() {
            let stderr = String::from_utf8_lossy(&output.stderr).trim().to_owned();
            return Err(format!(
                "Raw ALSA hardware enumeration exited with {}: {stderr}",
                output.status
            ));
        }
        Ok(parse_aplay_hardware(&String::from_utf8_lossy(
            &output.stdout,
        )))
    }
}

pub struct OutputDeviceCatalog {
    adapter: Box<dyn AlsaOutputDeviceAdapter>,
    devices: Vec<OutputDevice>,
    selected: Option<OutputDevice>,
}

impl OutputDeviceCatalog {
    pub fn new(adapter: Box<dyn AlsaOutputDeviceAdapter>) -> Self {
        Self {
            adapter,
            devices: Vec::new(),
            selected: None,
        }
    }

    pub fn refresh(&mut self) -> Result<Vec<OutputDevice>, OutputDeviceError> {
        self.devices = self
            .adapter
            .list_raw_devices()
            .map_err(OutputDeviceError::EnumerationFailed)?;
        if let Some(selected) = self.selected.as_ref()
            && !self.devices.iter().any(|device| device.id == selected.id)
        {
            return Err(OutputDeviceError::SelectedDeviceDisconnected(
                selected.clone(),
            ));
        }
        Ok(self.devices.clone())
    }

    pub fn select(&mut self, device_id: &str) -> Result<OutputDevice, OutputDeviceError> {
        if !is_raw_hardware_id(device_id) {
            return Err(OutputDeviceError::NotRawHardware);
        }
        self.devices = self
            .adapter
            .list_raw_devices()
            .map_err(OutputDeviceError::EnumerationFailed)?;
        let device = self
            .devices
            .iter()
            .find(|device| device.id == device_id)
            .cloned()
            .ok_or(OutputDeviceError::Unavailable)?;
        self.selected = Some(device.clone());
        Ok(device)
    }

    pub fn selected(&self) -> Option<&OutputDevice> {
        self.selected.as_ref()
    }

    pub fn devices(&self) -> &[OutputDevice] {
        &self.devices
    }
}

fn is_raw_hardware_id(device_id: &str) -> bool {
    let Some(components) = device_id.strip_prefix("hw:") else {
        return false;
    };
    let mut components = components.split(',');
    matches!(components.next(), Some(card) if card.parse::<u32>().is_ok())
        && matches!(components.next(), Some(device) if device.parse::<u32>().is_ok())
        && components.next().is_none()
}

fn parse_aplay_hardware(output: &str) -> Vec<OutputDevice> {
    output.lines().filter_map(parse_aplay_device_line).collect()
}

fn parse_aplay_device_line(line: &str) -> Option<OutputDevice> {
    let line = line.trim();
    let card = line.strip_prefix("card ")?;
    let (card_number, remainder) = card.split_once(':')?;
    let device = remainder.split(", device ").nth(1)?;
    let (device_number, description) = device.split_once(':')?;
    let card_number = card_number.trim().parse::<u32>().ok()?;
    let device_number = device_number.trim().parse::<u32>().ok()?;
    Some(OutputDevice {
        id: format!("hw:{card_number},{device_number}"),
        name: description.trim().to_owned(),
    })
}

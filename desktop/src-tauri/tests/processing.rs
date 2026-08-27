use earthly_audio_desktop::processing::{
    EqualizerPreset, FileProcessingSettingsStorage, MpvProcessingConfiguration,
    ProcessingController, ProcessingProfile, ProcessingSettingsStorage, ReplayGainMode,
};

struct EmptySettingsStorage;

struct FailingSettingsStorage;

impl ProcessingSettingsStorage for EmptySettingsStorage {
    fn load(&self) -> Result<Option<String>, String> {
        Ok(None)
    }

    fn save(&self, _value: &str) -> Result<(), String> {
        Ok(())
    }
}

impl ProcessingSettingsStorage for FailingSettingsStorage {
    fn load(&self) -> Result<Option<String>, String> {
        Ok(None)
    }

    fn save(&self, _value: &str) -> Result<(), String> {
        Err("controlled settings failure".to_owned())
    }
}

fn in_memory_controller() -> ProcessingController {
    ProcessingController::open(Box::new(EmptySettingsStorage))
        .expect("open in-memory Processing Profile")
}

#[test]
fn direct_profile_exposes_unity_processing_to_mpv() {
    let controller = in_memory_controller();

    assert_eq!(controller.state().profile, ProcessingProfile::Direct);
    assert_eq!(
        controller.mpv_configuration(),
        MpvProcessingConfiguration {
            volume_percent: 100.0,
            replay_gain_mode: ReplayGainMode::Off,
            audio_filters: Vec::new(),
        }
    );
}

#[test]
fn failed_persistence_does_not_mutate_observable_processing_state() {
    let mut controller = ProcessingController::open(Box::new(FailingSettingsStorage))
        .expect("open failing storage fixture");

    let error = controller
        .set_software_volume(0.4)
        .expect_err("surface persistence failure");

    assert!(error.contains("controlled settings failure"));
    assert_eq!(controller.state().profile, ProcessingProfile::Direct);
    assert_eq!(controller.state().software_volume, 1.0);
}

#[test]
fn user_processing_changes_switch_profile_and_persist() {
    let settings_path = temporary_path("processing-settings.json");
    let storage = FileProcessingSettingsStorage::new(settings_path.clone());
    let mut controller =
        ProcessingController::open(Box::new(storage)).expect("open Processing Profile");

    controller
        .set_software_volume(0.45)
        .expect("set software volume");
    assert_eq!(
        controller.state().transition_notice.as_deref(),
        Some("Software volume requires the Processed Profile.")
    );
    controller
        .enable_replay_gain(ReplayGainMode::Album)
        .expect("enable ReplayGain");
    controller
        .apply_equalizer_preset(EqualizerPreset::Vocal)
        .expect("apply EQ preset");

    assert_eq!(controller.state().profile, ProcessingProfile::Processed);
    assert_eq!(controller.mpv_configuration().audio_filters.len(), 10);

    let mut restored = ProcessingController::open(Box::new(FileProcessingSettingsStorage::new(
        settings_path.clone(),
    )))
    .expect("restore Processing Profile");
    assert_eq!(restored.state().software_volume, 0.45);
    assert_eq!(restored.state().replay_gain_mode, ReplayGainMode::Album);
    assert_eq!(restored.state().equalizer.preset, EqualizerPreset::Vocal);
    restored
        .set_equalizer_gain(0, 6.0)
        .expect("set custom EQ gain");
    assert_eq!(restored.state().equalizer.preset, EqualizerPreset::Custom);
    restored
        .set_profile(ProcessingProfile::Direct)
        .expect("restore Direct Profile");
    assert_eq!(
        restored.mpv_configuration(),
        MpvProcessingConfiguration {
            volume_percent: 100.0,
            replay_gain_mode: ReplayGainMode::Off,
            audio_filters: Vec::new(),
        }
    );

    std::fs::remove_file(settings_path).expect("remove settings fixture");
}

fn temporary_path(file_name: &str) -> std::path::PathBuf {
    let unique = format!(
        "earthly-audio-{}-{}-{}",
        std::process::id(),
        std::thread::current().name().unwrap_or("test"),
        file_name
    );
    std::env::temp_dir().join(unique)
}

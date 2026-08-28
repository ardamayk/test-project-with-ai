# Exclusive Output Manual Test Matrix

Use this matrix on Linux with at least one real USB DAC. Exclusive must automatically resolve the operating system's active PipeWire output to an enumerated raw ALSA `hw:` device. It must not show a device picker, use ALSA conversion plugins, claim mpv exclusive mode, or label the result bit-perfect.

Record the Desktop Client version, Linux distribution, kernel, ALSA version, PipeWire/WirePlumber versions, mpv version, DAC make/model/firmware, connection type, the selected system output, and the matching `hw:` identifier from `aplay -l` before each run.

## Player Bar menu

1. Start a Track in the Desktop Client.
2. Click the current output-mode text at the right side of the Player Bar.
3. Confirm that a small menu opens immediately above the button and contains exactly **Normal**, **Exclusive**, and **Adaptive**, with tight vertical spacing and no device list.
4. Select **Normal**, restart the Desktop Client, and confirm that Normal remains selected and audio follows changes made in the operating system's output selector.
5. Select **Adaptive** once, confirm that the system-wide warning remains visible, then select it again to enable the mode.

## Source and format coverage

For each format the DAC advertises, play a local library Track and record the observed Source, Decoder, System, Device, and Processing telemetry.

| Source | Expected behavior | Result |
| --- | --- | --- |
| PCM 44.1 kHz / 16-bit / stereo | After selecting Exclusive, playback starts on the raw `hw:` device mapped from the active system output. | Pass / Fail / Not supported |
| PCM 48 kHz / 24-bit / stereo | Same as above. | Pass / Fail / Not supported |
| PCM 88.2 or 96 kHz / 24-bit / stereo | Same as above. | Pass / Fail / Not supported |
| PCM 176.4 or 192 kHz / 24-bit / stereo | Same as above. | Pass / Fail / Not supported |
| One DAC-advertised unsupported rate or sample format | Exclusive fails with an actionable message and Normal becomes active. The UI does not continue to claim Exclusive. | Pass / Fail |

Unknown negotiated fields must remain `Unknown`; source metadata must not be copied into Device telemetry.

## Failure and hotplug coverage

| Scenario | Procedure | Expected behavior | Result |
| --- | --- | --- | --- |
| Busy hardware | Select a USB DAC as the system output, hold its `hw:` device open in another application, then select **Exclusive**. | An actionable busy-or-unsupported message appears and **Normal** is active. | Pass / Fail |
| Return to Normal | While Exclusive is active, select **Normal**. | mpv returns to PipeWire `audio-device=auto`, the mode persists as Normal, and audio follows the current system output. | Pass / Fail |
| Change active output | Select DAC A as the system output and enable Exclusive. Return to Normal, select DAC B in the operating system, then enable Exclusive again. | Each Exclusive selection resolves the current system output; DAC B is used on the second selection without an in-app device choice. | Pass / Fail |
| Bluetooth active output | Select a Bluetooth sink in the operating system, then select **Exclusive**. | An actionable unsupported-output message appears and **Normal** remains active. | Pass / Fail |
| Disconnect during playback | Start Exclusive playback, then unplug the active DAC. | Playback pauses and reports the disconnection. Selecting **Normal** returns playback to the active system output. | Pass / Fail |
| Restart Desktop Client | Enable Exclusive, quit, and relaunch with the same DAC still selected as the system output. Repeat with the output removed or changed to Bluetooth. | The available active ALSA output is resolved again at startup. If it cannot be resolved or opened, startup recovers to persisted Normal and never falsely claims Exclusive. | Pass / Fail |

Attach relevant local logs for failures, but redact server tokens and private media-proxy URLs before sharing results.

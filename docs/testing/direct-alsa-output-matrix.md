# Direct ALSA Output Manual Test Matrix

Use this matrix on Linux with at least one real USB DAC. Direct ALSA Output must select an enumerated raw `hw:` device. It must not use ALSA conversion plugins, claim mpv exclusive mode, or label the result bit-perfect.

Record the Desktop Client version, Linux distribution, kernel, ALSA version, mpv version, DAC make/model/firmware, connection type, and the `hw:` identifier from `aplay -l` before each run.

## Source and format coverage

For each format the DAC advertises, play a local library Track and record the observed Source, Decoder, System, Device, and Processing telemetry.

| Source | Expected behavior | Result |
| --- | --- | --- |
| PCM 44.1 kHz / 16-bit / stereo | Playback starts on the selected `hw:` device; System reports `Bypassed`; Device reports the selected DAC and negotiated format when mpv exposes it. | Pass / Fail / Not supported |
| PCM 48 kHz / 24-bit / stereo | Same as above. | Pass / Fail / Not supported |
| PCM 88.2 or 96 kHz / 24-bit / stereo | Same as above. | Pass / Fail / Not supported |
| PCM 176.4 or 192 kHz / 24-bit / stereo | Same as above. | Pass / Fail / Not supported |
| One DAC-advertised unsupported rate or sample format | Playback pauses or does not start; the Desktop Client asks for another device or explicit System Output. It does not silently reroute. | Pass / Fail |

Unknown negotiated fields must remain `Unknown`; source metadata must not be copied into Device telemetry.

## Failure and hotplug coverage

| Scenario | Procedure | Expected behavior | Result |
| --- | --- | --- | --- |
| Busy hardware | Hold the selected `hw:` device open in another application, then select it in the Desktop Client. | An actionable busy-or-unsupported prompt appears. Playback does not silently switch to System Output. | Pass / Fail |
| Explicit fallback | From the busy-or-unsupported prompt, choose **Use System Output**. | The active Playback Session changes to System Output only after the click; PipeWire telemetry replaces bypass telemetry. | Pass / Fail |
| Disconnect during playback | Start Direct ALSA playback, then unplug the DAC. | Playback pauses. A disconnected-device prompt asks for another device or explicit System Output. | Pass / Fail |
| Reconnect | Reconnect the same DAC and refresh devices. | The raw device appears again. Playback does not resume or reroute without a user selection. | Pass / Fail |
| Select another DAC | With the first DAC disconnected, select another enumerated `hw:` device. | Direct ALSA Output uses the newly selected raw device and clears the prompt after successful negotiation. | Pass / Fail |
| Restart Desktop Client | Select a DAC, quit, reconnect it, and relaunch. | The selected device identifier is restored locally. If unavailable or busy, startup remains actionable and does not silently claim Direct ALSA playback. | Pass / Fail |

Attach relevant local logs for failures, but redact server tokens and private media-proxy URLs before sharing results.

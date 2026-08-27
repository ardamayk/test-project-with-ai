# Navidrome Replacement

Navidrome Replacement is a self-hosted music app for browsing local library content and listening to live radio streams.

## Language

### Playback

**Music Server**:
The authoritative source for library content and user-shared data. It serves Playback Clients but does not produce sound for them.
_Avoid_: Player, Playback Client, audio device

**Server Connection**:
The Desktop Client's configured relationship to one Music Server URL. The initial product does not discover servers or retain an offline library.
_Avoid_: Server Profile, local server, offline library

**Local-only Deployment**:
A Music Server deployment bound to the local machine's loopback interface. The initial product does not authenticate Playback Clients and cannot be reached from other devices.
_Avoid_: LAN deployment, internet-ready deployment, trusted client identity

**Server Capability**:
A named behavior advertised by a Music Server so a Playback Client can adapt when their release versions differ.
_Avoid_: Client version, feature assumption

**Playback Client**:
An app instance on a user device that browses the Music Server and plays audio on that device. Web Client and Desktop Client are Playback Client variants.
_Avoid_: Music Server, UI, frontend

**Web Client**:
A Playback Client used through a web browser. Its playback remains local to the browser's device.
_Avoid_: Desktop Client, server player

**Desktop Client**:
An installed Playback Client with native playback capability. It connects to a separate Music Server and does not contain or start one.
_Avoid_: Music Server, standalone server, web wrapper

**Player**:
The audio engine local to a Playback Client. Each Playback Client owns its Player and playback position independently.
_Avoid_: Music Server, Playback Client, queue

**Native Playback**:
Playback performed by the Desktop Client's local Player through the host operating system's audio facilities.
_Avoid_: Browser playback, server playback

**Output Mode**:
The device-local routing policy used by a Native Player: System Output, Direct ALSA Output, or Adaptive System Rate.
_Avoid_: Processing Profile, Output Device

**Processing Profile**:
The device-local signal-processing policy used independently from Output Mode: Direct Profile or Processed Profile.
_Avoid_: Output Mode, audio route

**Direct Profile**:
A Native Playback profile that keeps software volume at unity and disables ReplayGain, EQ, and other signal processing while seeking a source-to-output format match.
_Avoid_: Bit-perfect guarantee, Processed Profile

**Processed Profile**:
A Native Playback profile that permits software volume, ReplayGain, EQ, or other transformations of the decoded signal.
_Avoid_: Direct Profile, format-preserving playback

**Format Match**:
An observed match between decoded source parameters and parameters written to the operating system audio interface, with no known signal processing enabled. It is evidence of a direct path, not proof of mathematically bit-identical DAC output.
_Avoid_: Bit-perfect guarantee, source quality

**System Output**:
The default Native Playback output mode in which the Player submits source parameters to PipeWire and PipeWire decides the graph and device formats.
_Avoid_: Direct ALSA Output, guaranteed format match

**Direct ALSA Output**:
An advanced Native Playback output mode that opens a selected ALSA `hw:` Output Device without PipeWire or ALSA conversion plugins. Device availability and exact source-format support determine whether playback can start.
_Avoid_: PipeWire exclusive mode, guaranteed device availability

**Adaptive System Rate**:
An experimental Native Playback output mode that temporarily forces the local PipeWire graph to each Playback Source's sample rate and therefore changes processing for every application on the device.
_Avoid_: Bitrate matching, bit-depth matching, isolated application setting

**Playback Telemetry**:
The separately reported source, decoder, system graph, Output Device, and active processing parameters used to explain whether conversion or processing is known to occur. Unknown device information remains explicitly unknown rather than being inferred from source metadata.
_Avoid_: Generic quality badge, bit-perfect proof

**ReplayGain Metadata**:
Track or album loudness adjustment metadata owned by a library Track and indexed by the Music Server. Its availability is separate from whether a Playback Client enables ReplayGain.
_Avoid_: Player preference, volume setting, inferred loudness

**Playback Source**:
Content a Player can load, currently a library Track, saved Radio Station, or Catalog Preview.
_Avoid_: Queue item, playlist, Output Device

**Playback Parity**:
The promise that Web and Desktop Clients can play the same Playback Sources even though their Players use different audio engines.
_Avoid_: Identical audio engine, shared Playback Session

**Playback Session**:
The transient, device-local state of one Player, including the current item, playhead, play/pause state, volume, shuffle, and repeat mode.
_Avoid_: Queue, playlist, shared playback state

**Playback Session Snapshot**:
The device-local record used to restore the last item and playhead in a paused state after the Desktop Client relaunches or to recover once from a Player crash.
_Avoid_: Shared playback state, automatic playback

**Background Playback**:
Continuation of the Desktop Client's Playback Session after its main window closes. An explicit application quit ends the session.
_Avoid_: Server playback, remote playback

**Queue**:
A user-owned ordered list of library Tracks shared across that user's Playback Clients. Each Player consumes the shared Queue through its own independent Playback Session.
_Avoid_: Playlist, Playback Session, play history

**Queue Synchronization**:
Propagation of Queue changes to every active Playback Client without synchronizing their independent Playback Sessions.
_Avoid_: Playback synchronization, shared playhead

**Queue Revision**:
The version of a Queue state used to detect and reject mutations based on stale data.
_Avoid_: Track position, Playback Session version

**Output Device**:
The audio endpoint on the Playback Client's device through which its Player produces sound.
_Avoid_: Playback Client, Music Server, stream source

### Radio

**Radio Station**:
A user-specific, user-managed or imported live internet audio stream. It is not a local library track and does not have stable per-track ownership in the app.
_Avoid_: Channel, radio track, stream item

**Imported Radio Station**:
A Radio Station copied from an external directory into the local collection. After import, the local copy is the source of truth.
_Avoid_: Remote station, live catalog item

**Radio Browser Catalog Entry**:
A discoverable external internet radio listing returned by Radio Browser. It can be previewed or imported, but it is not a local Radio Station until import succeeds.
_Avoid_: Radio Station, saved station, local stream

**Radio Browser Catalog Option**:
A country or tag/genre option proxied from Radio Browser for discover filtering. Country options use Radio Browser's ISO country code when available so the UI can render a flag next to the country name.
_Avoid_: Local filter preset, saved preference

**Catalog Preview**:
Temporary Live Playback of a Radio Browser Catalog Entry before import. It lets the user audition the stream without creating a local Radio Station.
_Avoid_: Auto-import, saved playback, library playback

**AAC Family**:
Discover filter meaning AAC and AAC+ catalog entries. Users expect AAC to include AAC+ because Radio Browser reports both as closely related codec labels.
_Avoid_: Exact AAC-only filter

**Local Station Health**:
Reachability status measured by this app for a saved Radio Station. It is separate from Radio Browser's catalog health because manual stations do not have external directory metadata.
_Avoid_: Radio Browser health, stream quality, Now Playing

**Radio Browser Health**:
Directory metadata from Radio Browser such as last successful check. It is useful for ranking confidence but does not guarantee that the stream will play through the browser or this app at preview time.
_Avoid_: Playback guarantee, local station health

**Live Playback**:
Playback of an unbounded radio stream. It is separate from queue playback because it has no fixed duration, stable track list, or seek target.
_Avoid_: Queue playback, radio queue item

**Now Playing**:
Current program or song metadata reported by a Radio Station while its live stream is playing. It is advisory metadata, not a local library Track, and may be stale or absent.
_Avoid_: Track, queued track, library metadata

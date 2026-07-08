# Navidrome Replacement

Navidrome Replacement is a self-hosted music app for browsing local library content and listening to live radio streams.

## Language

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

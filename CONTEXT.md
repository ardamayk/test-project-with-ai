# Navidrome Replacement

Navidrome Replacement is a self-hosted music app for browsing local library content and listening to live radio streams.

## Language

**Radio Station**:
A user-specific, user-managed or imported live internet audio stream. It is not a local library track and does not have stable per-track ownership in the app.
_Avoid_: Channel, radio track, stream item

**Imported Radio Station**:
A Radio Station copied from an external directory into the local collection. After import, the local copy is the source of truth.
_Avoid_: Remote station, live catalog item

**Live Playback**:
Playback of an unbounded radio stream. It is separate from queue playback because it has no fixed duration, stable track list, or seek target.
_Avoid_: Queue playback, radio queue item

**Now Playing**:
Current program or song metadata reported by a Radio Station while its live stream is playing. It is advisory metadata, not a local library Track, and may be stale or absent.
_Avoid_: Track, queued track, library metadata

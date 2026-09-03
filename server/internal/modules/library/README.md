# Library module

Read and delete APIs for Artists, Albums, Tracks, and Album Artwork, plus the
media-inspection seam shared with Managed Import.

The module never ingests files. The legacy startup scanner is retired (ADR
0015): Managed Import owns ingestion and Legacy Tracks change only through an
explicit Library Migration. `MUSIC_PATHS` is used solely to locate existing
Legacy Tracks for playback, deletion, and migration.

The deprecated `POST /api/v1/library/scan` and `GET /api/v1/library/scan/status`
routes stay mounted for API v1 compatibility (ADR 0006). The trigger always
answers `410 legacy_scan_retired`; the status is permanently `idle`.

`Store.SeedLegacyTrack` remains only so tests can construct Legacy Tracks that
behave like rows indexed before Managed Import became authoritative.

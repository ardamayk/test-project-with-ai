-- +goose Up
-- Managed Import is the only library model (ADR 0016). The Library Migration
-- that carried legacy rows into Managed Storage has run to completion, so the
-- remaining hidden Legacy Track rows and every legacy-only table go away.

-- Legacy Track rows were hidden by the migration cutover (missing_at set) and
-- their audio files were removed by Legacy Source Cleanup. Cascades clear
-- track_sources, track_artists, track_genres, playlist_tracks and playback_queue.
DELETE FROM tracks WHERE id IN (
    SELECT track_id FROM track_sources WHERE source_kind = 'legacy'
);
DELETE FROM tracks WHERE id NOT IN (SELECT track_id FROM track_sources);

-- Albums, artists and genres that only legacy rows referenced.
DELETE FROM album_artwork WHERE album_id NOT IN (SELECT DISTINCT album_id FROM tracks);
DELETE FROM album_release_identifiers WHERE album_id NOT IN (SELECT DISTINCT album_id FROM tracks);
DELETE FROM album_artists WHERE album_id NOT IN (SELECT DISTINCT album_id FROM tracks);
DELETE FROM albums WHERE id NOT IN (SELECT DISTINCT album_id FROM tracks);
DELETE FROM artists WHERE id NOT IN (SELECT artist_id FROM albums)
    AND id NOT IN (SELECT artist_id FROM album_artists)
    AND id NOT IN (SELECT artist_id FROM track_artists);
DELETE FROM genres WHERE id NOT IN (SELECT genre_id FROM track_genres);

DROP TRIGGER IF EXISTS validate_legacy_album_artwork_source_insert;
DROP TRIGGER IF EXISTS validate_legacy_album_artwork_source_update;
DROP TRIGGER IF EXISTS clear_legacy_album_artwork_source_track_album_update;
DROP TABLE IF EXISTS legacy_album_artwork_metadata;
DROP TABLE IF EXISTS legacy_album_genres;
DROP TABLE IF EXISTS legacy_track_identities;
DROP TABLE IF EXISTS legacy_album_identities;
DROP TABLE IF EXISTS legacy_artist_identities;
DROP TABLE IF EXISTS legacy_library_backfill_state;
DROP TABLE IF EXISTS legacy_migration_copies;
DROP TABLE IF EXISTS legacy_migration_sources;

-- +goose Down
-- The legacy subsystem is not restorable: its rows were derived from files the
-- Music Server no longer locates. Rolling back only recreates the empty tables
-- so older binaries can start.
CREATE TABLE IF NOT EXISTS legacy_migration_sources (
    track_id TEXT PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
    source_track_id TEXT NOT NULL,
    source_file_path TEXT NOT NULL,
    source_sha256 TEXT NOT NULL,
    migrated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    cleaned_at DATETIME
);
CREATE TABLE IF NOT EXISTS legacy_library_backfill_state (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    completed_at DATETIME
);

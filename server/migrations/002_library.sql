-- +goose Up
CREATE TABLE IF NOT EXISTS artists (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    name_sort TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_artists_name_sort ON artists(name_sort);

CREATE TABLE IF NOT EXISTS albums (
    id TEXT PRIMARY KEY,
    artist_id TEXT NOT NULL REFERENCES artists(id),
    title TEXT NOT NULL,
    title_sort TEXT NOT NULL,
    year INTEGER,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_albums_artist_id ON albums(artist_id);
CREATE INDEX IF NOT EXISTS idx_albums_title_sort ON albums(title_sort);

CREATE TABLE IF NOT EXISTS tracks (
    id TEXT PRIMARY KEY,
    album_id TEXT NOT NULL REFERENCES albums(id),
    title TEXT NOT NULL,
    title_sort TEXT NOT NULL,
    artist_name TEXT NOT NULL,
    track_no INTEGER,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    format TEXT NOT NULL,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    file_path TEXT NOT NULL UNIQUE,
    file_mtime INTEGER NOT NULL DEFAULT 0,
    missing_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tracks_album_id ON tracks(album_id);
CREATE INDEX IF NOT EXISTS idx_tracks_title_sort ON tracks(title_sort);

CREATE TABLE IF NOT EXISTS scan_jobs (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'idle',
    started_at DATETIME,
    finished_at DATETIME,
    scanned INTEGER NOT NULL DEFAULT 0,
    added INTEGER NOT NULL DEFAULT 0,
    updated INTEGER NOT NULL DEFAULT 0,
    removed INTEGER NOT NULL DEFAULT 0,
    error_message TEXT
);

CREATE TABLE IF NOT EXISTS playback_queue (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    position INTEGER NOT NULL,
    track_id TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    UNIQUE(user_id, position)
);

CREATE INDEX IF NOT EXISTS idx_playback_queue_user_id ON playback_queue(user_id);

-- +goose Down
DROP TABLE IF EXISTS playback_queue;
DROP TABLE IF EXISTS scan_jobs;
DROP TABLE IF EXISTS tracks;
DROP TABLE IF EXISTS albums;
DROP TABLE IF EXISTS artists;

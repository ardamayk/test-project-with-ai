-- +goose Up
CREATE TABLE IF NOT EXISTS radio_stations (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    name TEXT NOT NULL,
    stream_url TEXT NOT NULL,
    homepage_url TEXT NOT NULL DEFAULT '',
    favicon_url TEXT NOT NULL DEFAULT '',
    country TEXT NOT NULL DEFAULT '',
    language TEXT NOT NULL DEFAULT '',
    tags TEXT NOT NULL DEFAULT '[]',
    codec TEXT NOT NULL DEFAULT '',
    bitrate INTEGER NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT 'manual',
    external_id TEXT NOT NULL DEFAULT '',
    is_favorite INTEGER NOT NULL DEFAULT 0,
    position INTEGER NOT NULL DEFAULT 0,
    last_now_playing_title TEXT NOT NULL DEFAULT '',
    last_now_playing_artist TEXT NOT NULL DEFAULT '',
    last_now_playing_raw TEXT NOT NULL DEFAULT '',
    last_now_playing_updated_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_radio_stations_user_id ON radio_stations(user_id);
CREATE INDEX IF NOT EXISTS idx_radio_stations_user_order ON radio_stations(user_id, is_favorite, position, name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_radio_stations_external
    ON radio_stations(user_id, source, external_id)
    WHERE external_id != '';

-- +goose Down
DROP TABLE IF EXISTS radio_stations;

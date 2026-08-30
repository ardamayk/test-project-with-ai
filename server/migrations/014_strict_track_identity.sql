-- +goose Up
ALTER TABLE artists ADD COLUMN name_normalized TEXT;

CREATE UNIQUE INDEX idx_artists_name_normalized
    ON artists(name_normalized)
    WHERE name_normalized IS NOT NULL;

ALTER TABLE albums ADD COLUMN identity_key TEXT;
ALTER TABLE albums ADD COLUMN release_date TEXT;
ALTER TABLE albums ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0);

CREATE UNIQUE INDEX idx_albums_identity_key
    ON albums(identity_key)
    WHERE identity_key IS NOT NULL;

ALTER TABLE tracks ADD COLUMN disc_no INTEGER CHECK (disc_no > 0);
ALTER TABLE tracks ADD COLUMN track_total INTEGER CHECK (track_total > 0);
ALTER TABLE tracks ADD COLUMN disc_total INTEGER CHECK (disc_total > 0);
ALTER TABLE tracks ADD COLUMN channel_count INTEGER CHECK (channel_count > 0);
ALTER TABLE tracks ADD COLUMN bitrate_bps INTEGER CHECK (bitrate_bps > 0);
ALTER TABLE tracks ADD COLUMN codec TEXT;
ALTER TABLE tracks ADD COLUMN container TEXT;
ALTER TABLE tracks ADD COLUMN sample_format TEXT;
ALTER TABLE tracks ADD COLUMN identity_key TEXT;
ALTER TABLE tracks ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0);

CREATE UNIQUE INDEX idx_tracks_active_album_position
    ON tracks(album_id, COALESCE(disc_no, 1), track_no)
    WHERE missing_at IS NULL AND identity_key IS NOT NULL AND track_no IS NOT NULL;

CREATE INDEX idx_tracks_album_identity_key
    ON tracks(album_id, identity_key)
    WHERE identity_key IS NOT NULL;

-- +goose StatementBegin
CREATE TRIGGER validate_strict_track_position_insert
BEFORE INSERT ON tracks
WHEN
    (NEW.disc_no IS NOT NULL AND NEW.track_no IS NULL) OR
    (NEW.track_total IS NOT NULL AND (NEW.track_no IS NULL OR NEW.track_total < NEW.track_no)) OR
    (NEW.disc_total IS NOT NULL AND (NEW.disc_no IS NULL OR NEW.disc_total < NEW.disc_no))
BEGIN
    SELECT RAISE(ABORT, 'invalid strict Track position totals');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER validate_strict_track_position_update
BEFORE UPDATE OF track_no, disc_no, track_total, disc_total ON tracks
WHEN
    (NEW.disc_no IS NOT NULL AND NEW.track_no IS NULL) OR
    (NEW.track_total IS NOT NULL AND (NEW.track_no IS NULL OR NEW.track_total < NEW.track_no)) OR
    (NEW.disc_total IS NOT NULL AND (NEW.disc_no IS NULL OR NEW.disc_total < NEW.disc_no))
BEGIN
    SELECT RAISE(ABORT, 'invalid strict Track position totals');
END;
-- +goose StatementEnd

CREATE TABLE track_sources (
    id TEXT PRIMARY KEY,
    track_id TEXT NOT NULL UNIQUE REFERENCES tracks(id) ON DELETE CASCADE,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('legacy', 'managed')),
    file_path TEXT NOT NULL UNIQUE,
    content_sha256 TEXT CHECK (
        content_sha256 IS NULL OR (
            length(content_sha256) = 64 AND
            content_sha256 NOT GLOB '*[^0-9a-f]*'
        )
    ),
    source_format TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT managed_source_hash_required CHECK (
        source_kind != 'managed' OR content_sha256 IS NOT NULL
    )
);

CREATE UNIQUE INDEX idx_track_sources_content_sha256
    ON track_sources(content_sha256)
    WHERE content_sha256 IS NOT NULL;

CREATE INDEX idx_track_sources_source_kind
    ON track_sources(source_kind);

CREATE TABLE track_artists (
    track_id TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    artist_id TEXT NOT NULL REFERENCES artists(id),
    position INTEGER NOT NULL CHECK (position >= 0),
    PRIMARY KEY (track_id, artist_id),
    UNIQUE (track_id, position)
);

CREATE INDEX idx_track_artists_artist_id
    ON track_artists(artist_id);

CREATE TABLE album_artists (
    album_id TEXT NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    artist_id TEXT NOT NULL REFERENCES artists(id),
    position INTEGER NOT NULL CHECK (position >= 0),
    PRIMARY KEY (album_id, artist_id),
    UNIQUE (album_id, position)
);

CREATE INDEX idx_album_artists_artist_id
    ON album_artists(artist_id);

CREATE TABLE genres (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    name_normalized TEXT NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE track_genres (
    track_id TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    genre_id TEXT NOT NULL REFERENCES genres(id),
    position INTEGER NOT NULL CHECK (position >= 0),
    PRIMARY KEY (track_id, genre_id),
    UNIQUE (track_id, position)
);

CREATE INDEX idx_track_genres_genre_id
    ON track_genres(genre_id);

CREATE TABLE album_release_identifiers (
    album_id TEXT NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    scheme TEXT NOT NULL CHECK (length(scheme) > 0),
    value TEXT NOT NULL CHECK (length(value) > 0),
    PRIMARY KEY (album_id, scheme),
    UNIQUE (scheme, value)
);

CREATE TABLE album_artwork (
    id TEXT PRIMARY KEY,
    album_id TEXT NOT NULL UNIQUE REFERENCES albums(id) ON DELETE CASCADE,
    source_track_id TEXT NOT NULL REFERENCES tracks(id),
    content_sha256 TEXT NOT NULL CHECK (
        length(content_sha256) = 64 AND
        content_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    media_type TEXT NOT NULL CHECK (media_type IN ('image/jpeg', 'image/png', 'image/webp')),
    width INTEGER NOT NULL CHECK (width > 0),
    height INTEGER NOT NULL CHECK (height > 0),
    encoded_size_bytes INTEGER NOT NULL CHECK (
        encoded_size_bytes > 0 AND encoded_size_bytes <= 20971520
    ),
    file_path TEXT NOT NULL UNIQUE,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (width * height <= 50000000)
);

CREATE INDEX idx_album_artwork_content_sha256
    ON album_artwork(content_sha256);

-- +goose StatementBegin
CREATE TRIGGER validate_album_artwork_source_insert
BEFORE INSERT ON album_artwork
WHEN NOT EXISTS (
    SELECT 1
    FROM tracks
    WHERE id = NEW.source_track_id AND album_id = NEW.album_id
)
BEGIN
    SELECT RAISE(ABORT, 'Album Artwork source Track must belong to the Album');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER validate_album_artwork_source_update
BEFORE UPDATE OF album_id, source_track_id ON album_artwork
WHEN NOT EXISTS (
    SELECT 1
    FROM tracks
    WHERE id = NEW.source_track_id AND album_id = NEW.album_id
)
BEGIN
    SELECT RAISE(ABORT, 'Album Artwork source Track must belong to the Album');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER validate_album_artwork_source_track_album_update
BEFORE UPDATE OF album_id ON tracks
WHEN EXISTS (
    SELECT 1
    FROM album_artwork
    WHERE source_track_id = OLD.id AND album_id != NEW.album_id
)
BEGIN
    SELECT RAISE(ABORT, 'Album Artwork source Track must remain in the Album');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS validate_album_artwork_source_track_album_update;
DROP TRIGGER IF EXISTS validate_album_artwork_source_update;
DROP TRIGGER IF EXISTS validate_album_artwork_source_insert;
DROP TABLE IF EXISTS album_artwork;
DROP TABLE IF EXISTS album_release_identifiers;
DROP TABLE IF EXISTS track_genres;
DROP TABLE IF EXISTS genres;
DROP TABLE IF EXISTS album_artists;
DROP TABLE IF EXISTS track_artists;
DROP TABLE IF EXISTS track_sources;

DROP TRIGGER IF EXISTS validate_strict_track_position_update;
DROP TRIGGER IF EXISTS validate_strict_track_position_insert;
DROP INDEX IF EXISTS idx_tracks_album_identity_key;
DROP INDEX IF EXISTS idx_tracks_active_album_position;
ALTER TABLE tracks DROP COLUMN revision;
ALTER TABLE tracks DROP COLUMN identity_key;
ALTER TABLE tracks DROP COLUMN sample_format;
ALTER TABLE tracks DROP COLUMN container;
ALTER TABLE tracks DROP COLUMN codec;
ALTER TABLE tracks DROP COLUMN bitrate_bps;
ALTER TABLE tracks DROP COLUMN channel_count;
ALTER TABLE tracks DROP COLUMN disc_total;
ALTER TABLE tracks DROP COLUMN track_total;
ALTER TABLE tracks DROP COLUMN disc_no;

DROP INDEX IF EXISTS idx_albums_identity_key;
ALTER TABLE albums DROP COLUMN revision;
ALTER TABLE albums DROP COLUMN release_date;
ALTER TABLE albums DROP COLUMN identity_key;

DROP INDEX IF EXISTS idx_artists_name_normalized;
ALTER TABLE artists DROP COLUMN name_normalized;

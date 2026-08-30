-- +goose Up
CREATE TABLE legacy_artist_identities (
    artist_id TEXT PRIMARY KEY REFERENCES artists(id) ON DELETE CASCADE,
    normalized_name TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_legacy_artist_identities_normalized_name
    ON legacy_artist_identities(normalized_name);

CREATE TABLE legacy_album_identities (
    album_id TEXT PRIMARY KEY REFERENCES albums(id) ON DELETE CASCADE,
    identity_key TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_legacy_album_identities_identity_key
    ON legacy_album_identities(identity_key);

CREATE TABLE legacy_track_identities (
    track_id TEXT PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
    identity_key TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_legacy_track_identities_identity_key
    ON legacy_track_identities(identity_key);

CREATE TABLE legacy_album_artwork_metadata (
    album_id TEXT PRIMARY KEY REFERENCES albums(id) ON DELETE CASCADE,
    source_track_id TEXT REFERENCES tracks(id) ON DELETE SET NULL,
    content_sha256 TEXT NOT NULL CHECK (
        length(content_sha256) = 64 AND
        content_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    media_type TEXT CHECK (media_type IN ('image/jpeg', 'image/png', 'image/webp')),
    width INTEGER CHECK (width > 0),
    height INTEGER CHECK (height > 0),
    encoded_size_bytes INTEGER NOT NULL CHECK (encoded_size_bytes > 0),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK ((width IS NULL) = (height IS NULL))
);

CREATE INDEX idx_legacy_album_artwork_metadata_content_sha256
    ON legacy_album_artwork_metadata(content_sha256);

-- +goose StatementBegin
CREATE TRIGGER validate_legacy_album_artwork_source_insert
BEFORE INSERT ON legacy_album_artwork_metadata
WHEN NEW.source_track_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM tracks
    WHERE id = NEW.source_track_id AND album_id = NEW.album_id
)
BEGIN
    SELECT RAISE(ABORT, 'Legacy Album Artwork source Track must belong to the Album');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER validate_legacy_album_artwork_source_update
BEFORE UPDATE OF album_id, source_track_id ON legacy_album_artwork_metadata
WHEN NEW.source_track_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM tracks
    WHERE id = NEW.source_track_id AND album_id = NEW.album_id
)
BEGIN
    SELECT RAISE(ABORT, 'Legacy Album Artwork source Track must belong to the Album');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER clear_legacy_album_artwork_source_track_album_update
AFTER UPDATE OF album_id ON tracks
WHEN EXISTS (
    SELECT 1 FROM legacy_album_artwork_metadata
    WHERE source_track_id = OLD.id AND album_id != NEW.album_id
)
BEGIN
    UPDATE legacy_album_artwork_metadata
    SET source_track_id = NULL
    WHERE source_track_id = OLD.id;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS clear_legacy_album_artwork_source_track_album_update;
DROP TRIGGER IF EXISTS validate_legacy_album_artwork_source_update;
DROP TRIGGER IF EXISTS validate_legacy_album_artwork_source_insert;
DROP TABLE IF EXISTS legacy_album_artwork_metadata;
DROP TABLE IF EXISTS legacy_track_identities;
DROP TABLE IF EXISTS legacy_album_identities;
DROP TABLE IF EXISTS legacy_artist_identities;

-- +goose Up
CREATE TABLE permanent_track_deletions (
    track_id TEXT PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
    file_path TEXT NOT NULL,
    content_sha256 TEXT NOT NULL CHECK (
        length(content_sha256) = 64 AND
        content_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS permanent_track_deletions;

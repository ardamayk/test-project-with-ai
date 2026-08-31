-- +goose Up
CREATE TABLE managed_import_jobs (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('uploading', 'awaiting_confirmation', 'committed', 'failed')),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    original_filename TEXT,
    staged_file_path TEXT UNIQUE,
    content_sha256 TEXT CHECK (
        content_sha256 IS NULL OR (
            length(content_sha256) = 64 AND
            content_sha256 NOT GLOB '*[^0-9a-f]*'
        )
    ),
    track_id TEXT REFERENCES tracks(id),
    error_code TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (status != 'awaiting_confirmation' OR (
        original_filename IS NOT NULL AND
        staged_file_path IS NOT NULL AND
        content_sha256 IS NOT NULL
    )),
    CHECK (status != 'committed' OR track_id IS NOT NULL)
);

CREATE INDEX idx_managed_import_jobs_status
    ON managed_import_jobs(status);

-- +goose Down
DROP TABLE IF EXISTS managed_import_jobs;

-- +goose Up
CREATE TABLE managed_import_history (
    import_id TEXT PRIMARY KEY,
    started_at DATETIME NOT NULL,
    completed_at DATETIME NOT NULL,
    result_code TEXT NOT NULL CHECK (result_code IN ('completed', 'partially_completed', 'failed', 'canceled')),
    total_count INTEGER NOT NULL CHECK (total_count >= 0),
    imported_count INTEGER NOT NULL CHECK (imported_count >= 0),
    rejected_count INTEGER NOT NULL CHECK (rejected_count >= 0),
    failed_count INTEGER NOT NULL CHECK (failed_count >= 0),
    replaced_count INTEGER NOT NULL CHECK (replaced_count >= 0),
    not_attempted_count INTEGER NOT NULL CHECK (not_attempted_count >= 0),
    canceled_count INTEGER NOT NULL CHECK (canceled_count >= 0),
    CHECK (
        total_count = imported_count + rejected_count + failed_count +
            replaced_count + not_attempted_count + canceled_count
    )
);

CREATE INDEX idx_managed_import_history_completed
    ON managed_import_history(completed_at DESC);

CREATE TABLE managed_import_history_files (
    import_id TEXT NOT NULL REFERENCES managed_import_history(import_id) ON DELETE CASCADE,
    file_id TEXT NOT NULL,
    job_id TEXT NOT NULL,
    safe_filename TEXT,
    started_at DATETIME NOT NULL,
    completed_at DATETIME NOT NULL,
    content_sha256 TEXT CHECK (
        content_sha256 IS NULL OR (
            length(content_sha256) = 64 AND
            content_sha256 NOT GLOB '*[^0-9a-f]*'
        )
    ),
    result_code TEXT NOT NULL,
    created_track_id TEXT,
    replaced_track_id TEXT,
    position INTEGER NOT NULL CHECK (position >= 0),
    PRIMARY KEY (import_id, file_id),
    UNIQUE (import_id, job_id)
);

CREATE INDEX idx_managed_import_history_files_import
    ON managed_import_history_files(import_id, position);

CREATE TABLE managed_import_canceled_files (
    batch_id TEXT NOT NULL REFERENCES managed_import_batches(id) ON DELETE CASCADE,
    file_id TEXT NOT NULL,
    job_id TEXT NOT NULL,
    safe_filename TEXT,
    started_at DATETIME NOT NULL,
    completed_at DATETIME NOT NULL,
    content_sha256 TEXT CHECK (
        content_sha256 IS NULL OR (
            length(content_sha256) = 64 AND
            content_sha256 NOT GLOB '*[^0-9a-f]*'
        )
    ),
    position INTEGER NOT NULL CHECK (position >= 0),
    PRIMARY KEY (batch_id, file_id),
    UNIQUE (batch_id, job_id)
);

-- +goose Down
DROP TABLE IF EXISTS managed_import_canceled_files;
DROP INDEX IF EXISTS idx_managed_import_history_files_import;
DROP TABLE IF EXISTS managed_import_history_files;
DROP INDEX IF EXISTS idx_managed_import_history_completed;
DROP TABLE IF EXISTS managed_import_history;

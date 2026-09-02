-- +goose Up
CREATE TABLE managed_import_batches (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('uploading', 'confirming', 'completed')),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_managed_import_batches_status
    ON managed_import_batches(status);

ALTER TABLE managed_import_jobs ADD COLUMN batch_id TEXT REFERENCES managed_import_batches(id);
ALTER TABLE managed_import_jobs ADD COLUMN outcome TEXT CHECK (
    outcome IS NULL OR outcome IN ('imported', 'rejected', 'failed', 'replaced', 'not_attempted')
);
ALTER TABLE managed_import_jobs ADD COLUMN preview_json TEXT;
ALTER TABLE managed_import_jobs ADD COLUMN error_field TEXT;
ALTER TABLE managed_import_jobs ADD COLUMN error_reason TEXT;
ALTER TABLE managed_import_jobs ADD COLUMN selected INTEGER NOT NULL DEFAULT 1 CHECK (selected IN (0, 1));
ALTER TABLE managed_import_jobs ADD COLUMN batch_position INTEGER NOT NULL DEFAULT 0 CHECK (batch_position >= 0);
ALTER TABLE managed_import_jobs ADD COLUMN upload_size_bytes INTEGER NOT NULL DEFAULT 0 CHECK (upload_size_bytes >= 0);

CREATE INDEX idx_managed_import_jobs_batch
    ON managed_import_jobs(batch_id);
CREATE UNIQUE INDEX idx_managed_import_jobs_batch_position
    ON managed_import_jobs(batch_id, batch_position)
    WHERE batch_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_managed_import_jobs_batch;
DROP INDEX IF EXISTS idx_managed_import_jobs_batch_position;
ALTER TABLE managed_import_jobs DROP COLUMN error_reason;
ALTER TABLE managed_import_jobs DROP COLUMN selected;
ALTER TABLE managed_import_jobs DROP COLUMN batch_position;
ALTER TABLE managed_import_jobs DROP COLUMN upload_size_bytes;
ALTER TABLE managed_import_jobs DROP COLUMN error_field;
ALTER TABLE managed_import_jobs DROP COLUMN preview_json;
ALTER TABLE managed_import_jobs DROP COLUMN outcome;
ALTER TABLE managed_import_jobs DROP COLUMN batch_id;
DROP INDEX IF EXISTS idx_managed_import_batches_status;
DROP TABLE IF EXISTS managed_import_batches;

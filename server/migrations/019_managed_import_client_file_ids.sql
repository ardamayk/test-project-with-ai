-- +goose Up
ALTER TABLE managed_import_jobs ADD COLUMN client_file_id TEXT;

CREATE UNIQUE INDEX idx_managed_import_jobs_batch_client_file
    ON managed_import_jobs(batch_id, client_file_id)
    WHERE batch_id IS NOT NULL AND client_file_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_managed_import_jobs_batch_client_file;
ALTER TABLE managed_import_jobs DROP COLUMN client_file_id;

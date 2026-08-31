-- +goose Up
ALTER TABLE managed_import_jobs
    ADD COLUMN validation_progress INTEGER NOT NULL DEFAULT 0
    CHECK (validation_progress >= 0 AND validation_progress <= 100);

-- +goose Down
ALTER TABLE managed_import_jobs DROP COLUMN validation_progress;

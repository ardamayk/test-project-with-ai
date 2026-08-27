-- +goose Up
CREATE TABLE IF NOT EXISTS playback_queue_state (
    user_id TEXT PRIMARY KEY,
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0)
);

-- +goose Down
DROP TABLE IF EXISTS playback_queue_state;

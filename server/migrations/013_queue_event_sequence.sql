-- +goose Up
ALTER TABLE playback_queue_state
ADD COLUMN event_sequence INTEGER NOT NULL DEFAULT 0 CHECK (event_sequence >= 0);

-- +goose Down
ALTER TABLE playback_queue_state DROP COLUMN event_sequence;

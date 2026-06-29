-- +goose Up
ALTER TABLE tracks ADD COLUMN sample_rate_hz INTEGER;

ALTER TABLE tracks ADD COLUMN bit_depth INTEGER;

-- +goose Down
ALTER TABLE tracks DROP COLUMN bit_depth;
ALTER TABLE tracks DROP COLUMN sample_rate_hz;

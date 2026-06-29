-- +goose Up
ALTER TABLE albums ADD COLUMN genre TEXT;

-- +goose Down
ALTER TABLE albums DROP COLUMN genre;

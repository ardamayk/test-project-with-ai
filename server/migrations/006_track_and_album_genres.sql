-- +goose Up
ALTER TABLE tracks ADD COLUMN genre TEXT;

ALTER TABLE albums ADD COLUMN genres TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE albums DROP COLUMN genres;
ALTER TABLE tracks DROP COLUMN genre;

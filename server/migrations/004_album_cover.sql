-- +goose Up
ALTER TABLE albums ADD COLUMN cover_mime TEXT;
ALTER TABLE albums ADD COLUMN cover_data BLOB;

-- +goose Down
ALTER TABLE albums DROP COLUMN cover_data;
ALTER TABLE albums DROP COLUMN cover_mime;

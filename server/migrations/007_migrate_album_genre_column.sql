-- +goose Up
UPDATE albums
SET genres = json_array(genre)
WHERE genre IS NOT NULL AND genre != '';

ALTER TABLE albums DROP COLUMN genre;

-- +goose Down
ALTER TABLE albums ADD COLUMN genre TEXT;

UPDATE albums
SET genre = json_extract(genres, '$[0]')
WHERE genres IS NOT NULL AND genres != '[]';

ALTER TABLE albums DROP COLUMN genres;

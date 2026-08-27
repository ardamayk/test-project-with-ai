-- +goose Up
ALTER TABLE tracks ADD COLUMN replaygain_track_gain_db REAL;
ALTER TABLE tracks ADD COLUMN replaygain_track_peak REAL;
ALTER TABLE tracks ADD COLUMN replaygain_album_gain_db REAL;
ALTER TABLE tracks ADD COLUMN replaygain_album_peak REAL;

-- +goose Down
ALTER TABLE tracks DROP COLUMN replaygain_album_peak;
ALTER TABLE tracks DROP COLUMN replaygain_album_gain_db;
ALTER TABLE tracks DROP COLUMN replaygain_track_peak;
ALTER TABLE tracks DROP COLUMN replaygain_track_gain_db;

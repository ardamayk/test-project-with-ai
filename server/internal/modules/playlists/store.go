package playlists

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/google/uuid"
)

const DefaultFavoritesName = "Favorites"

var ErrNotFound = errors.New("playlist not found")

type Playlist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsDefault  bool   `json:"isDefault"`
	TrackCount int    `json:"trackCount"`
}

type PlaylistList struct {
	Items []Playlist `json:"items"`
	Total int        `json:"total"`
}

type PlaylistDetail struct {
	Playlist
	Tracks []library.Track `json:"tracks"`
}

type Store struct {
	db     *sql.DB
	tracks library.TrackReader
}

func NewStore(db *sql.DB, tracks library.TrackReader) *Store {
	return &Store{db: db, tracks: tracks}
}

func (s *Store) ensureDefaultFavorites(ctx context.Context, userID string) (Playlist, error) {
	if _, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO playlists (id, user_id, name, is_default)
		VALUES (?, ?, ?, 1)`,
		uuid.NewString(), userID, DefaultFavoritesName,
	); err != nil {
		return Playlist{}, err
	}
	return s.getPlaylistByName(ctx, userID, DefaultFavoritesName)
}

func (s *Store) GetDefaultFavorites(ctx context.Context, userID string) (Playlist, error) {
	return s.ensureDefaultFavorites(ctx, userID)
}

func (s *Store) ListPlaylists(ctx context.Context, userID string) (PlaylistList, error) {
	if _, err := s.ensureDefaultFavorites(ctx, userID); err != nil {
		return PlaylistList{}, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.is_default, COUNT(pt.track_id)
		FROM playlists p
		LEFT JOIN playlist_tracks pt ON pt.playlist_id = p.id
		WHERE p.user_id = ?
		GROUP BY p.id, p.name, p.is_default
		ORDER BY p.is_default DESC, p.name COLLATE NOCASE`, userID)
	if err != nil {
		return PlaylistList{}, err
	}
	defer func() { _ = rows.Close() }()

	items := []Playlist{}
	for rows.Next() {
		p, err := scanPlaylist(rows)
		if err != nil {
			return PlaylistList{}, err
		}
		items = append(items, p)
	}
	return PlaylistList{Items: items, Total: len(items)}, rows.Err()
}

func (s *Store) CreatePlaylist(ctx context.Context, userID, name string) (Playlist, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Playlist{}, fmt.Errorf("playlist name is required")
	}
	if _, err := s.ensureDefaultFavorites(ctx, userID); err != nil {
		return Playlist{}, err
	}
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO playlists (id, user_id, name, is_default)
		VALUES (?, ?, ?, 0)`,
		id, userID, name,
	)
	if err != nil {
		return Playlist{}, err
	}
	playlist, err := s.GetPlaylist(ctx, userID, id)
	if err != nil {
		return Playlist{}, err
	}
	return playlist.Playlist, nil
}

func (s *Store) GetPlaylist(ctx context.Context, userID, playlistID string) (PlaylistDetail, error) {
	if _, err := s.ensureDefaultFavorites(ctx, userID); err != nil {
		return PlaylistDetail{}, err
	}
	playlist, err := s.getPlaylist(ctx, userID, playlistID)
	if err != nil {
		return PlaylistDetail{}, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT pt.track_id
		FROM playlist_tracks pt
		WHERE pt.playlist_id = ?
		ORDER BY pt.position`, playlistID)
	if err != nil {
		return PlaylistDetail{}, err
	}
	defer func() { _ = rows.Close() }()

	tracks := []library.Track{}
	for rows.Next() {
		var trackID string
		if err := rows.Scan(&trackID); err != nil {
			return PlaylistDetail{}, err
		}
		track, err := s.tracks.GetTrack(ctx, trackID)
		if err != nil {
			continue
		}
		tracks = append(tracks, track)
	}
	return PlaylistDetail{Playlist: playlist, Tracks: tracks}, rows.Err()
}

func (s *Store) AddTrack(ctx context.Context, userID, playlistID, trackID string) (PlaylistDetail, error) {
	if _, err := s.GetPlaylist(ctx, userID, playlistID); err != nil {
		return PlaylistDetail{}, err
	}
	if _, err := s.tracks.GetTrack(ctx, trackID); err != nil {
		return PlaylistDetail{}, library.ErrNotFound
	}

	var maxPos sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MAX(position) FROM playlist_tracks WHERE playlist_id = ?`,
		playlistID,
	).Scan(&maxPos); err != nil {
		return PlaylistDetail{}, err
	}
	pos := 0
	if maxPos.Valid {
		pos = int(maxPos.Int64) + 1
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO playlist_tracks (playlist_id, track_id, position)
		VALUES (?, ?, ?)`, playlistID, trackID, pos); err != nil {
		return PlaylistDetail{}, err
	}
	return s.GetPlaylist(ctx, userID, playlistID)
}

func (s *Store) RemoveTrack(ctx context.Context, userID, playlistID, trackID string) (PlaylistDetail, error) {
	detail, err := s.GetPlaylist(ctx, userID, playlistID)
	if err != nil {
		return PlaylistDetail{}, err
	}
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM playlist_tracks WHERE playlist_id = ? AND track_id = ?`,
		playlistID, trackID,
	); err != nil {
		return PlaylistDetail{}, err
	}
	if detail.IsDefault {
		return s.GetPlaylist(ctx, userID, playlistID)
	}

	var remaining int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM playlist_tracks WHERE playlist_id = ?`,
		playlistID,
	).Scan(&remaining); err != nil {
		return PlaylistDetail{}, err
	}
	if remaining == 0 {
		if _, err := s.db.ExecContext(ctx,
			`DELETE FROM playlists WHERE id = ? AND user_id = ? AND is_default = 0`,
			playlistID, userID,
		); err != nil {
			return PlaylistDetail{}, err
		}
		detail.TrackCount = 0
		detail.Tracks = []library.Track{}
		return detail, nil
	}
	return s.GetPlaylist(ctx, userID, playlistID)
}

func (s *Store) getPlaylist(ctx context.Context, userID, playlistID string) (Playlist, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.is_default, COUNT(pt.track_id)
		FROM playlists p
		LEFT JOIN playlist_tracks pt ON pt.playlist_id = p.id
		WHERE p.user_id = ? AND p.id = ?
		GROUP BY p.id, p.name, p.is_default`, userID, playlistID)
	p, err := scanPlaylist(row)
	if err == sql.ErrNoRows {
		return Playlist{}, ErrNotFound
	}
	return p, err
}

func (s *Store) getPlaylistByName(ctx context.Context, userID, name string) (Playlist, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.is_default, COUNT(pt.track_id)
		FROM playlists p
		LEFT JOIN playlist_tracks pt ON pt.playlist_id = p.id
		WHERE p.user_id = ? AND p.name = ?
		GROUP BY p.id, p.name, p.is_default`, userID, name)
	return scanPlaylist(row)
}

type playlistScanner interface {
	Scan(dest ...any) error
}

func scanPlaylist(scanner playlistScanner) (Playlist, error) {
	var p Playlist
	var isDefault int
	if err := scanner.Scan(&p.ID, &p.Name, &isDefault, &p.TrackCount); err != nil {
		return Playlist{}, err
	}
	p.IsDefault = isDefault != 0
	return p, nil
}

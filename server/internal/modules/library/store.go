package library

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Artist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AlbumCount int    `json:"albumCount,omitempty"`
}

type Album struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	ArtistID   string `json:"artistId"`
	ArtistName string `json:"artistName"`
	Year       *int   `json:"year,omitempty"`
	TrackCount int    `json:"trackCount,omitempty"`
}

type Track struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	ArtistName string `json:"artistName"`
	AlbumID    string `json:"albumId"`
	AlbumTitle string `json:"albumTitle,omitempty"`
	TrackNo    *int   `json:"trackNo,omitempty"`
	DurationMs int    `json:"durationMs"`
	Format     string `json:"format"`
	SizeBytes  int64  `json:"sizeBytes,omitempty"`
	FilePath   string `json:"-"`
}

type AlbumDetail struct {
	Album
	Tracks []Track `json:"tracks"`
}

type ArtistList struct {
	Items []Artist `json:"items"`
	Total int      `json:"total"`
}

type AlbumList struct {
	Items []Album `json:"items"`
	Total int     `json:"total"`
}

type TrackList struct {
	Items []Track `json:"items"`
	Total int     `json:"total"`
}

type ScanStatus struct {
	Status     string     `json:"status"`
	Scanned    int        `json:"scanned"`
	Added      int        `json:"added"`
	Updated    int        `json:"updated"`
	Removed    int        `json:"removed"`
	Error      string     `json:"error,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) ListArtists(ctx context.Context, limit, offset int, q string) (ArtistList, error) {
	filter := ""
	args := []any{}
	if q != "" {
		filter = " AND a.name LIKE ?"
		args = append(args, "%"+q+"%")
	}

	var total int
	countQuery := `SELECT COUNT(DISTINCT a.id) FROM artists a
		INNER JOIN albums al ON al.artist_id = a.id
		INNER JOIN tracks t ON t.album_id = al.id AND t.missing_at IS NULL` + filter
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return ArtistList{}, fmt.Errorf("count artists: %w", err)
	}

	query := `SELECT a.id, a.name, COUNT(DISTINCT al.id) AS album_count
		FROM artists a
		INNER JOIN albums al ON al.artist_id = a.id
		INNER JOIN tracks t ON t.album_id = al.id AND t.missing_at IS NULL
		WHERE 1=1` + filter + `
		GROUP BY a.id, a.name
		ORDER BY a.name_sort
		LIMIT ? OFFSET ?`
	queryArgs := append(append([]any{}, args...), limit, offset)

	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return ArtistList{}, fmt.Errorf("list artists: %w", err)
	}
	defer rows.Close()

	items := []Artist{}
	for rows.Next() {
		var a Artist
		if err := rows.Scan(&a.ID, &a.Name, &a.AlbumCount); err != nil {
			return ArtistList{}, err
		}
		items = append(items, a)
	}
	return ArtistList{Items: items, Total: total}, rows.Err()
}

func (s *Store) ListAlbums(ctx context.Context, limit, offset int, artistID, q string) (AlbumList, error) {
	where := "WHERE t.missing_at IS NULL"
	args := []any{}
	if artistID != "" {
		where += " AND al.artist_id = ?"
		args = append(args, artistID)
	}
	if q != "" {
		where += " AND al.title LIKE ?"
		args = append(args, "%"+q+"%")
	}

	var total int
	countQuery := `SELECT COUNT(DISTINCT al.id) FROM albums al
		INNER JOIN tracks t ON t.album_id = al.id ` + where
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return AlbumList{}, fmt.Errorf("count albums: %w", err)
	}

	query := `SELECT al.id, al.title, al.artist_id, ar.name, al.year, COUNT(t.id) AS track_count
		FROM albums al
		INNER JOIN artists ar ON ar.id = al.artist_id
		INNER JOIN tracks t ON t.album_id = al.id AND t.missing_at IS NULL
		` + where + `
		GROUP BY al.id, al.title, al.artist_id, ar.name, al.year
		ORDER BY al.title_sort
		LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return AlbumList{}, fmt.Errorf("list albums: %w", err)
	}
	defer rows.Close()

	items := []Album{}
	for rows.Next() {
		var a Album
		var year sql.NullInt64
		if err := rows.Scan(&a.ID, &a.Title, &a.ArtistID, &a.ArtistName, &year, &a.TrackCount); err != nil {
			return AlbumList{}, err
		}
		if year.Valid {
			y := int(year.Int64)
			a.Year = &y
		}
		items = append(items, a)
	}
	return AlbumList{Items: items, Total: total}, rows.Err()
}

func (s *Store) GetAlbum(ctx context.Context, albumID string) (AlbumDetail, error) {
	var a AlbumDetail
	var year sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT al.id, al.title, al.artist_id, ar.name, al.year,
			(SELECT COUNT(*) FROM tracks t WHERE t.album_id = al.id AND t.missing_at IS NULL)
		FROM albums al
		INNER JOIN artists ar ON ar.id = al.artist_id
		WHERE al.id = ?`, albumID,
	).Scan(&a.ID, &a.Title, &a.ArtistID, &a.ArtistName, &year, &a.TrackCount)
	if err == sql.ErrNoRows {
		return AlbumDetail{}, ErrNotFound
	}
	if err != nil {
		return AlbumDetail{}, fmt.Errorf("get album: %w", err)
	}
	if year.Valid {
		y := int(year.Int64)
		a.Year = &y
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, artist_name, album_id, track_no, duration_ms, format, size_bytes
		FROM tracks
		WHERE album_id = ? AND missing_at IS NULL
		ORDER BY COALESCE(track_no, 9999), title_sort`, albumID)
	if err != nil {
		return AlbumDetail{}, err
	}
	defer rows.Close()

	a.Tracks = []Track{}
	for rows.Next() {
		t, err := scanTrackRow(rows, a.Title)
		if err != nil {
			return AlbumDetail{}, err
		}
		a.Tracks = append(a.Tracks, t)
	}
	return a, rows.Err()
}

func (s *Store) ListTracks(ctx context.Context, limit, offset int, q string) (TrackList, error) {
	where := "WHERE missing_at IS NULL"
	args := []any{}
	if q != "" {
		where += " AND (title LIKE ? OR artist_name LIKE ?)"
		args = append(args, "%"+q+"%", "%"+q+"%")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tracks "+where, args...).Scan(&total); err != nil {
		return TrackList{}, err
	}

	query := `SELECT t.id, t.title, t.artist_name, t.album_id, al.title, t.track_no, t.duration_ms, t.format, t.size_bytes
		FROM tracks t
		INNER JOIN albums al ON al.id = t.album_id
		` + where + `
		ORDER BY t.title_sort
		LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return TrackList{}, err
	}
	defer rows.Close()

	items := []Track{}
	for rows.Next() {
		t, err := scanTrackRowWithAlbum(rows)
		if err != nil {
			return TrackList{}, err
		}
		items = append(items, t)
	}
	return TrackList{Items: items, Total: total}, rows.Err()
}

func (s *Store) GetTrack(ctx context.Context, trackID string) (Track, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT t.id, t.title, t.artist_name, t.album_id, al.title, t.track_no, t.duration_ms, t.format, t.size_bytes, t.file_path
		FROM tracks t
		INNER JOIN albums al ON al.id = t.album_id
		WHERE t.id = ? AND t.missing_at IS NULL`, trackID)

	var t Track
	var trackNo sql.NullInt64
	var albumTitle string
	err := row.Scan(&t.ID, &t.Title, &t.ArtistName, &t.AlbumID, &albumTitle, &trackNo, &t.DurationMs, &t.Format, &t.SizeBytes, &t.FilePath)
	if err == sql.ErrNoRows {
		return Track{}, ErrNotFound
	}
	if err != nil {
		return Track{}, err
	}
	t.AlbumTitle = albumTitle
	if trackNo.Valid {
		n := int(trackNo.Int64)
		t.TrackNo = &n
	}
	return t, nil
}

func (s *Store) GetTrackFilePath(ctx context.Context, trackID string) (string, error) {
	var path string
	err := s.db.QueryRowContext(ctx,
		`SELECT file_path FROM tracks WHERE id = ? AND missing_at IS NULL`, trackID,
	).Scan(&path)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return path, err
}

func (s *Store) UpsertFromScan(ctx context.Context, meta FileMetadata) (added, updated bool, err error) {
	existingID, existingMtime, err := s.findTrackByPath(ctx, meta.Path)
	if err != nil {
		return false, false, err
	}

	mtime := meta.ModTime.Unix()
	if existingID != "" {
		if existingMtime == mtime {
			if err := s.markTrackPresent(ctx, existingID); err != nil {
				return false, false, err
			}
			return false, false, nil
		}
		if err := s.updateTrack(ctx, existingID, meta, mtime); err != nil {
			return false, false, err
		}
		if err := s.markTrackPresent(ctx, existingID); err != nil {
			return false, false, err
		}
		return false, true, nil
	}

	if err := s.insertTrack(ctx, meta, mtime); err != nil {
		return false, false, err
	}
	return true, false, nil
}

func (s *Store) findTrackByPath(ctx context.Context, path string) (id string, mtime int64, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT id, file_mtime FROM tracks WHERE file_path = ?`, path,
	).Scan(&id, &mtime)
	if err == sql.ErrNoRows {
		return "", 0, nil
	}
	return id, mtime, err
}

func (s *Store) insertTrack(ctx context.Context, meta FileMetadata, mtime int64) error {
	artistID, err := s.upsertArtist(ctx, meta.Artist)
	if err != nil {
		return err
	}
	albumID, err := s.upsertAlbum(ctx, artistID, meta.Album, meta.Year)
	if err != nil {
		return err
	}

	trackID := uuid.NewString()
	trackNo := nullableInt(meta.TrackNo)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tracks (id, album_id, title, title_sort, artist_name, track_no, duration_ms, format, size_bytes, file_path, file_mtime)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		trackID, albumID, meta.Title, sortKey(meta.Title), meta.Artist, trackNo, meta.DurationMs, meta.Format, meta.SizeBytes, meta.Path, mtime,
	)
	return err
}

func (s *Store) updateTrack(ctx context.Context, trackID string, meta FileMetadata, mtime int64) error {
	artistID, err := s.upsertArtist(ctx, meta.Artist)
	if err != nil {
		return err
	}
	albumID, err := s.upsertAlbum(ctx, artistID, meta.Album, meta.Year)
	if err != nil {
		return err
	}
	trackNo := nullableInt(meta.TrackNo)
	_, err = s.db.ExecContext(ctx, `
		UPDATE tracks SET album_id = ?, title = ?, title_sort = ?, artist_name = ?, track_no = ?,
			duration_ms = ?, format = ?, size_bytes = ?, file_mtime = ?, missing_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		albumID, meta.Title, sortKey(meta.Title), meta.Artist, trackNo, meta.DurationMs, meta.Format, meta.SizeBytes, mtime, trackID,
	)
	return err
}

func (s *Store) upsertArtist(ctx context.Context, name string) (string, error) {
	name = sanitizeName(name, "Unknown Artist")
	sort := sortKey(name)
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM artists WHERE name_sort = ?`, sort).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	id = uuid.NewString()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO artists (id, name, name_sort) VALUES (?, ?, ?)`, id, name, sort,
	)
	return id, err
}

func (s *Store) upsertAlbum(ctx context.Context, artistID, title string, year int) (string, error) {
	title = sanitizeName(title, "Unknown Album")
	sort := sortKey(title)
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM albums WHERE artist_id = ? AND title_sort = ?`, artistID, sort,
	).Scan(&id)
	if err == nil {
		if year > 0 {
			_, _ = s.db.ExecContext(ctx, `UPDATE albums SET year = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND year IS NULL`, year, id)
		}
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	id = uuid.NewString()
	var yearVal any
	if year > 0 {
		yearVal = year
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO albums (id, artist_id, title, title_sort, year) VALUES (?, ?, ?, ?, ?)`,
		id, artistID, title, sort, yearVal,
	)
	return id, err
}

func (s *Store) BeginScan(ctx context.Context) (string, error) {
	var running int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM scan_jobs WHERE status = 'running'`,
	).Scan(&running); err != nil {
		return "", err
	}
	if running > 0 {
		return "", ErrScanRunning
	}

	id := uuid.NewString()
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO scan_jobs (id, status, started_at, scanned, added, updated, removed)
		VALUES (?, 'running', ?, 0, 0, 0, 0)`, id, now)
	if err != nil {
		return "", err
	}
	if err := s.resetScanPresence(ctx); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) resetScanPresence(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tracks SET missing_at = NULL WHERE missing_at IS NOT NULL`)
	return err
}

func (s *Store) markTrackPresent(ctx context.Context, trackID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tracks SET missing_at = NULL WHERE id = ?`, trackID)
	return err
}

func (s *Store) MarkSeenPaths(ctx context.Context, paths map[string]struct{}) (removed int, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, file_path FROM tracks WHERE missing_at IS NULL`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			return 0, err
		}
		if _, ok := paths[path]; !ok {
			if _, err := s.db.ExecContext(ctx,
				`UPDATE tracks SET missing_at = CURRENT_TIMESTAMP WHERE id = ?`, id,
			); err != nil {
				return 0, err
			}
			removed++
		}
	}
	return removed, rows.Err()
}

func (s *Store) UpdateScanProgress(ctx context.Context, jobID string, scanned, added, updated, removed int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE scan_jobs SET scanned = ?, added = ?, updated = ?, removed = ?
		WHERE id = ?`, scanned, added, updated, removed, jobID)
	return err
}

func (s *Store) FinishScan(ctx context.Context, jobID, status, errMsg string, scanned, added, updated, removed int) error {
	now := time.Now().UTC()
	var errVal any
	if errMsg != "" {
		errVal = errMsg
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE scan_jobs SET status = ?, finished_at = ?, scanned = ?, added = ?, updated = ?, removed = ?, error_message = ?
		WHERE id = ?`, status, now, scanned, added, updated, removed, errVal, jobID)
	return err
}

func (s *Store) GetScanStatus(ctx context.Context) (ScanStatus, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT status, scanned, added, updated, removed, error_message, started_at, finished_at
		FROM scan_jobs ORDER BY started_at DESC LIMIT 1`)

	var st ScanStatus
	var errMsg sql.NullString
	var started, finished sql.NullTime
	err := row.Scan(&st.Status, &st.Scanned, &st.Added, &st.Updated, &st.Removed, &errMsg, &started, &finished)
	if err == sql.ErrNoRows {
		return ScanStatus{Status: "idle"}, nil
	}
	if err != nil {
		return ScanStatus{}, err
	}
	if errMsg.Valid {
		st.Error = errMsg.String
	}
	if started.Valid {
		t := started.Time
		st.StartedAt = &t
	}
	if finished.Valid {
		t := finished.Time
		st.FinishedAt = &t
	}
	return st, nil
}

func scanTrackRow(rows *sql.Rows, albumTitle string) (Track, error) {
	var t Track
	var trackNo sql.NullInt64
	err := rows.Scan(&t.ID, &t.Title, &t.ArtistName, &t.AlbumID, &trackNo, &t.DurationMs, &t.Format, &t.SizeBytes)
	if err != nil {
		return Track{}, err
	}
	t.AlbumTitle = albumTitle
	if trackNo.Valid {
		n := int(trackNo.Int64)
		t.TrackNo = &n
	}
	return t, nil
}

func scanTrackRowWithAlbum(rows *sql.Rows) (Track, error) {
	var t Track
	var trackNo sql.NullInt64
	err := rows.Scan(&t.ID, &t.Title, &t.ArtistName, &t.AlbumID, &t.AlbumTitle, &trackNo, &t.DurationMs, &t.Format, &t.SizeBytes)
	if err != nil {
		return Track{}, err
	}
	if trackNo.Valid {
		n := int(trackNo.Int64)
		t.TrackNo = &n
	}
	return t, nil
}

func nullableInt(v int) any {
	if v <= 0 {
		return nil
	}
	return v
}

package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	internaldb "github.com/ardam/navidrome-replacement/server/internal/db"
	"github.com/google/uuid"
)

type Artist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AlbumCount int    `json:"albumCount,omitempty"`
}

type ArtistCredit struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Genre struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ReleaseIdentifier struct {
	Scheme string `json:"scheme"`
	Value  string `json:"value"`
}

type AlbumArtworkMetadata struct {
	SourceTrackID string `json:"sourceTrackId,omitempty"`
	ContentSHA256 string `json:"contentSha256"`
	MediaType     string `json:"mediaType,omitempty"`
	Width         int    `json:"width,omitempty"`
	Height        int    `json:"height,omitempty"`
	SizeBytes     int64  `json:"sizeBytes"`
}

type Album struct {
	ID                 string                `json:"id"`
	Title              string                `json:"title"`
	ArtistID           string                `json:"artistId"`
	ArtistName         string                `json:"artistName"`
	AlbumArtists       []ArtistCredit        `json:"albumArtists"`
	Year               *int                  `json:"year,omitempty"`
	ReleaseDate        *string               `json:"releaseDate,omitempty"`
	ReleaseIdentifiers []ReleaseIdentifier   `json:"releaseIdentifiers"`
	TrackCount         int                   `json:"trackCount,omitempty"`
	Genres             []string              `json:"genres,omitempty"`
	GenreItems         []Genre               `json:"genreItems"`
	Artwork            *AlbumArtworkMetadata `json:"artwork,omitempty"`
}

type Track struct {
	ID           string             `json:"id"`
	Title        string             `json:"title"`
	ArtistName   string             `json:"artistName"`
	Artists      []ArtistCredit     `json:"artists"`
	AlbumID      string             `json:"albumId"`
	AlbumTitle   string             `json:"albumTitle,omitempty"`
	DiscNo       int                `json:"discNo"`
	TrackNo      *int               `json:"trackNo,omitempty"`
	TrackTotal   *int               `json:"trackTotal,omitempty"`
	DiscTotal    *int               `json:"discTotal,omitempty"`
	DurationMs   int                `json:"durationMs"`
	Format       string             `json:"format"`
	Codec        string             `json:"codec,omitempty"`
	Container    string             `json:"container,omitempty"`
	SampleFormat string             `json:"sampleFormat,omitempty"`
	SizeBytes    int64              `json:"sizeBytes,omitempty"`
	Genre        string             `json:"genre,omitempty"`
	Genres       []Genre            `json:"genres"`
	SampleRateHz int                `json:"sampleRateHz,omitempty"`
	BitDepth     int                `json:"bitDepth,omitempty"`
	ChannelCount int                `json:"channelCount,omitempty"`
	BitrateBps   int                `json:"bitrateBps,omitempty"`
	BitrateKbps  int                `json:"bitrateKbps,omitempty"`
	ReplayGain   ReplayGainMetadata `json:"replayGain"`
	FilePath     string             `json:"-"`
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

const INTERRUPTED_SCAN_ERROR = "scan interrupted by server restart"

type Store struct {
	db      storeDatabase
	beginTx func(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db, beginTx: db.BeginTx}
}

type storeDatabase interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func runStoreMutation[T any](ctx context.Context, store *Store, operation string, mutate func(*Store, *sql.Tx) (T, error)) (result T, err error) {
	tx, err := store.beginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin %s: %w", operation, err)
	}
	defer func() {
		rollbackErr := tx.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("roll back %s: %w", operation, rollbackErr))
		}
	}()
	result, err = mutate(&Store{db: tx}, tx)
	if err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit %s: %w", operation, err)
	}
	return result, nil
}

func (s *Store) ListArtists(ctx context.Context, limit, offset int, q string) (ArtistList, error) {
	filter := ""
	queryArgs := []any{}
	if q != "" {
		filter = " AND a.name LIKE ?"
		queryArgs = append(queryArgs, "%"+q+"%")
	}

	var total int
	countQuery := activeArtistAlbumsCTE + `SELECT COUNT(DISTINCT a.id) FROM artists a
		INNER JOIN active_artist_albums credits ON credits.artist_id = a.id
		WHERE 1=1` + filter
	if err := s.db.QueryRowContext(ctx, countQuery, queryArgs...).Scan(&total); err != nil {
		return ArtistList{}, fmt.Errorf("count Artists for query %q: %w", q, err)
	}

	query := activeArtistAlbumsCTE + `SELECT a.id, a.name, COUNT(credits.album_id) AS album_count
		FROM artists a
		INNER JOIN active_artist_albums credits ON credits.artist_id = a.id
		WHERE 1=1` + filter + `
		GROUP BY a.id, a.name
		ORDER BY a.name_sort
		LIMIT ? OFFSET ?`
	queryArgs = append(queryArgs, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return ArtistList{}, fmt.Errorf("list Artists for query %q: %w", q, err)
	}
	defer func() { _ = rows.Close() }()

	items := []Artist{}
	for rows.Next() {
		var artist Artist
		if err := rows.Scan(&artist.ID, &artist.Name, &artist.AlbumCount); err != nil {
			return ArtistList{}, fmt.Errorf("scan Artist list row for query %q: %w", q, err)
		}
		items = append(items, artist)
	}
	if err := rows.Err(); err != nil {
		return ArtistList{}, fmt.Errorf("iterate Artist list for query %q: %w", q, err)
	}
	return ArtistList{Items: items, Total: total}, nil
}

func (s *Store) ListAlbums(ctx context.Context, limit, offset int, artistID, q string) (AlbumList, error) {
	where := "WHERE 1 = 1"
	args := []any{}
	if artistID != "" {
		where += ` AND (EXISTS (
			SELECT 1 FROM album_artists filter_credit
			WHERE filter_credit.album_id = al.id AND filter_credit.artist_id = ?
		) OR EXISTS (
			SELECT 1 FROM visible_tracks filter_track
			INNER JOIN track_artists filter_track_credit ON filter_track_credit.track_id = filter_track.id
			WHERE filter_track.album_id = al.id
				AND filter_track_credit.artist_id = ?
		))`
		args = append(args, artistID, artistID)
	}
	if q != "" {
		where += ` AND (al.title LIKE ? OR EXISTS (
			SELECT 1 FROM album_artists search_credit
			INNER JOIN artists search_artist ON search_artist.id = search_credit.artist_id
			WHERE search_credit.album_id = al.id AND search_artist.name LIKE ?
		))`
		args = append(args, "%"+q+"%", "%"+q+"%")
	}

	var total int
	countQuery := `SELECT COUNT(DISTINCT al.id) FROM albums al
		INNER JOIN visible_tracks t ON t.album_id = al.id ` + where
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return AlbumList{}, fmt.Errorf("count Albums for Artist %q and query %q: %w", artistID, q, err)
	}

	query := `SELECT al.id, al.title, COALESCE(primary_artist.id, legacy_artist.id),
			COALESCE(primary_artist.name, legacy_artist.name), al.year, al.release_date,
			COALESCE(al.genres, '[]'), COUNT(t.id) AS track_count
		FROM albums al
		INNER JOIN artists legacy_artist ON legacy_artist.id = al.artist_id
		LEFT JOIN album_artists primary_credit ON primary_credit.album_id = al.id AND primary_credit.position = 0
		LEFT JOIN artists primary_artist ON primary_artist.id = primary_credit.artist_id
		INNER JOIN visible_tracks t ON t.album_id = al.id
		` + where + `
		GROUP BY al.id, al.title, primary_artist.id, primary_artist.name,
			legacy_artist.id, legacy_artist.name, al.year, al.release_date, al.genres
		ORDER BY ` + albumListOrderBy(artistID) + `
		LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return AlbumList{}, fmt.Errorf("list Albums for Artist %q and query %q: %w", artistID, q, err)
	}
	defer func() { _ = rows.Close() }()

	items := []Album{}
	for rows.Next() {
		var album Album
		var year sql.NullInt64
		var releaseDate sql.NullString
		var genresRaw sql.NullString
		if err := rows.Scan(&album.ID, &album.Title, &album.ArtistID, &album.ArtistName, &year, &releaseDate, &genresRaw, &album.TrackCount); err != nil {
			return AlbumList{}, fmt.Errorf("scan Album list row for Artist %q and query %q: %w", artistID, q, err)
		}
		setAlbumDates(&album, year, releaseDate)
		album.Genres = decodeGenres(genresRaw.String)
		items = append(items, album)
	}
	if err := rows.Err(); err != nil {
		return AlbumList{}, fmt.Errorf("iterate Album list for Artist %q and query %q: %w", artistID, q, err)
	}
	if err := s.enrichAlbums(ctx, items); err != nil {
		return AlbumList{}, fmt.Errorf("enrich Album list for Artist %q and query %q: %w", artistID, q, err)
	}
	return AlbumList{Items: items, Total: total}, nil
}

func (s *Store) GetAlbum(ctx context.Context, albumID string) (AlbumDetail, error) {
	var album AlbumDetail
	var year sql.NullInt64
	var releaseDate sql.NullString
	var genresRaw sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT al.id, al.title, COALESCE(primary_artist.id, legacy_artist.id),
			COALESCE(primary_artist.name, legacy_artist.name), al.year, al.release_date,
			COALESCE(al.genres, '[]'),
			(SELECT COUNT(*) FROM visible_tracks t WHERE t.album_id = al.id)
		FROM albums al
		INNER JOIN artists legacy_artist ON legacy_artist.id = al.artist_id
		LEFT JOIN album_artists primary_credit ON primary_credit.album_id = al.id AND primary_credit.position = 0
		LEFT JOIN artists primary_artist ON primary_artist.id = primary_credit.artist_id
		WHERE al.id = ?`, albumID,
	).Scan(&album.ID, &album.Title, &album.ArtistID, &album.ArtistName, &year, &releaseDate, &genresRaw, &album.TrackCount)
	if err == sql.ErrNoRows {
		return AlbumDetail{}, ErrNotFound
	}
	if err != nil {
		return AlbumDetail{}, fmt.Errorf("get Album %q: %w", albumID, err)
	}
	setAlbumDates(&album.Album, year, releaseDate)
	album.Genres = decodeGenres(genresRaw.String)

	rows, err := s.db.QueryContext(ctx, trackReadSelect+`
		WHERE t.album_id = ?
		ORDER BY COALESCE(t.disc_no, 1), COALESCE(t.track_no, 9999), t.title_sort, t.id`, albumID)
	if err != nil {
		return AlbumDetail{}, fmt.Errorf("list Album %q Tracks: %w", albumID, err)
	}
	defer func() { _ = rows.Close() }()

	album.Tracks = []Track{}
	for rows.Next() {
		track, err := scanExpandedTrack(rows)
		if err != nil {
			return AlbumDetail{}, fmt.Errorf("scan Album %q Track: %w", albumID, err)
		}
		album.Tracks = append(album.Tracks, track)
	}
	if err := rows.Err(); err != nil {
		return AlbumDetail{}, fmt.Errorf("iterate Album %q Tracks: %w", albumID, err)
	}
	albums := []Album{album.Album}
	if err := s.enrichAlbums(ctx, albums); err != nil {
		return AlbumDetail{}, fmt.Errorf("enrich Album %q: %w", albumID, err)
	}
	album.Album = albums[0]
	if err := s.enrichTracks(ctx, album.Tracks); err != nil {
		return AlbumDetail{}, fmt.Errorf("enrich Album %q Tracks: %w", albumID, err)
	}
	album.TrackCount = len(album.Tracks)
	return album, nil
}

func (s *Store) ListTracks(ctx context.Context, limit, offset int, q string) (TrackList, error) {
	where := "WHERE 1 = 1"
	args := []any{}
	if q != "" {
		where += ` AND (t.title LIKE ? OR al.title LIKE ? OR EXISTS (
			SELECT 1 FROM track_artists search_credit
			INNER JOIN artists search_artist ON search_artist.id = search_credit.artist_id
			WHERE search_credit.track_id = t.id AND search_artist.name LIKE ?
		) OR EXISTS (
			SELECT 1 FROM track_genres search_relation
			INNER JOIN genres search_genre ON search_genre.id = search_relation.genre_id
			WHERE search_relation.track_id = t.id AND search_genre.name LIKE ?
		))`
		args = append(args, "%"+q+"%", "%"+q+"%", "%"+q+"%", "%"+q+"%")
	}

	var total int
	countQuery := `SELECT COUNT(*) FROM visible_tracks t
		INNER JOIN albums al ON al.id = t.album_id
		` + where
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return TrackList{}, fmt.Errorf("count Tracks for query %q: %w", q, err)
	}

	query := trackReadSelect + where + `
		ORDER BY t.title_sort
		LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return TrackList{}, fmt.Errorf("list Tracks for query %q: %w", q, err)
	}
	defer func() { _ = rows.Close() }()

	items := []Track{}
	for rows.Next() {
		track, err := scanExpandedTrack(rows)
		if err != nil {
			return TrackList{}, fmt.Errorf("scan Track list row for query %q: %w", q, err)
		}
		items = append(items, track)
	}
	if err := rows.Err(); err != nil {
		return TrackList{}, fmt.Errorf("iterate Track list for query %q: %w", q, err)
	}
	if err := s.enrichTracks(ctx, items); err != nil {
		return TrackList{}, fmt.Errorf("enrich Track list for query %q: %w", q, err)
	}
	return TrackList{Items: items, Total: total}, nil
}

func (s *Store) GetTrack(ctx context.Context, trackID string) (Track, error) {
	row := s.db.QueryRowContext(ctx, trackReadSelect+`
		WHERE t.id = ?`, trackID)
	track, err := scanExpandedTrack(row)
	if err == sql.ErrNoRows {
		return Track{}, ErrNotFound
	}
	if err != nil {
		return Track{}, fmt.Errorf("get Track %q: %w", trackID, err)
	}
	tracks := []Track{track}
	if err := s.enrichTracks(ctx, tracks); err != nil {
		return Track{}, fmt.Errorf("enrich Track %q: %w", trackID, err)
	}
	return tracks[0], nil
}

func (s *Store) GetTrackFilePath(ctx context.Context, trackID string) (string, error) {
	var path string
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(track_sources.file_path, tracks.file_path)
		FROM visible_tracks tracks LEFT JOIN track_sources ON track_sources.track_id = tracks.id
		WHERE tracks.id = ?`, trackID,
	).Scan(&path)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return path, err
}

func (s *Store) UpsertFromScan(ctx context.Context, meta FileMetadata) (added, updated bool, err error) {
	result, err := runStoreMutation(ctx, s, "scanned Track upsert", func(store *Store, tx *sql.Tx) (upsertResult, error) {
		previousAlbumID, lookupErr := store.findAlbumByTrackPath(ctx, meta.Path)
		if lookupErr != nil {
			return upsertResult{}, lookupErr
		}
		added, updated, upsertErr := store.upsertFromScan(ctx, meta)
		if upsertErr != nil {
			return upsertResult{}, upsertErr
		}
		trackID, _, lookupErr := store.findTrackByPath(ctx, meta.Path)
		if lookupErr != nil {
			return upsertResult{}, fmt.Errorf("lookup synchronized Track: %w", lookupErr)
		}
		if syncErr := internaldb.SynchronizeLegacyTrack(ctx, tx, trackID); syncErr != nil {
			return upsertResult{}, syncErr
		}
		currentAlbumID, lookupErr := store.findAlbumByTrackPath(ctx, meta.Path)
		if lookupErr != nil {
			return upsertResult{}, fmt.Errorf("lookup synchronized Track Album: %w", lookupErr)
		}
		if previousAlbumID != "" && previousAlbumID != currentAlbumID {
			if syncErr := internaldb.SynchronizeLegacyAlbum(ctx, tx, previousAlbumID); syncErr != nil {
				return upsertResult{}, syncErr
			}
			if syncErr := internaldb.FinalizeLegacyRemoval(ctx, tx); syncErr != nil {
				return upsertResult{}, syncErr
			}
		}
		return upsertResult{added: added, updated: updated}, nil
	})
	return result.added, result.updated, err
}

type upsertResult struct {
	added   bool
	updated bool
}

func (s *Store) findAlbumByTrackPath(ctx context.Context, path string) (string, error) {
	var albumID string
	err := s.db.QueryRowContext(ctx, `SELECT album_id FROM tracks WHERE file_path = ?`, path).Scan(&albumID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return albumID, err
}

func (s *Store) upsertFromScan(ctx context.Context, meta FileMetadata) (added, updated bool, err error) {
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
			if len(meta.CoverData) > 0 {
				if err := s.setAlbumCoverByTrackIfMissing(ctx, existingID, meta.CoverMime, meta.CoverData); err != nil {
					return false, false, fmt.Errorf("set album cover: %w", err)
				}
			}
			if meta.Genre != "" {
				if err := s.setTrackGenre(ctx, existingID, meta.Genre); err != nil {
					return false, false, fmt.Errorf("set track genre: %w", err)
				}
				var albumID string
				if err := s.db.QueryRowContext(ctx,
					`SELECT album_id FROM tracks WHERE id = ?`, existingID,
				).Scan(&albumID); err != nil {
					return false, false, fmt.Errorf("lookup album for genre: %w", err)
				}
				if err := s.recomputeAlbumGenres(ctx, albumID); err != nil {
					return false, false, fmt.Errorf("recompute album genres: %w", err)
				}
			}
			if meta.DurationMs > 0 {
				_ = s.updateTrackDurationIfZero(ctx, existingID, meta.DurationMs)
			}
			_ = s.updateTrackAudioFormatIfZero(ctx, existingID, meta)
			if err := s.reconcileTrackAlbum(ctx, existingID, meta); err != nil {
				return false, false, fmt.Errorf("reconcile track album: %w", err)
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
	artistID, err := s.upsertArtist(ctx, meta.AlbumArtist)
	if err != nil {
		return err
	}
	albumID, err := s.upsertAlbum(ctx, artistID, meta.Album, meta.Year, meta.CoverMime, meta.CoverData)
	if err != nil {
		return err
	}

	trackID := uuid.NewString()
	trackNo := nullableInt(meta.TrackNo)
	var genreVal any
	if meta.Genre != "" {
		genreVal = meta.Genre
	}
	sampleRateVal := nullableInt(meta.SampleRateHz)
	bitDepthVal := nullableInt(meta.BitDepth)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tracks (id, album_id, title, title_sort, artist_name, track_no, duration_ms, format, size_bytes, file_path, file_mtime, genre, sample_rate_hz, bit_depth,
			replaygain_track_gain_db, replaygain_track_peak, replaygain_album_gain_db, replaygain_album_peak)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		trackID, albumID, meta.Title, sortKey(meta.Title), meta.Artist, trackNo, meta.DurationMs, meta.Format, meta.SizeBytes, meta.Path, mtime, genreVal, sampleRateVal, bitDepthVal,
		meta.ReplayGain.TrackGainDB, meta.ReplayGain.TrackPeak, meta.ReplayGain.AlbumGainDB, meta.ReplayGain.AlbumPeak,
	)
	if err != nil {
		return err
	}
	return s.recomputeAlbumGenres(ctx, albumID)
}

func (s *Store) updateTrack(ctx context.Context, trackID string, meta FileMetadata, mtime int64) error {
	var previousAlbumID string
	if err := s.db.QueryRowContext(ctx,
		`SELECT album_id FROM tracks WHERE id = ?`, trackID,
	).Scan(&previousAlbumID); err != nil {
		return err
	}

	artistID, err := s.upsertArtist(ctx, meta.AlbumArtist)
	if err != nil {
		return err
	}
	albumID, err := s.upsertAlbum(ctx, artistID, meta.Album, meta.Year, meta.CoverMime, meta.CoverData)
	if err != nil {
		return err
	}
	trackNo := nullableInt(meta.TrackNo)
	var genreVal any
	if meta.Genre != "" {
		genreVal = meta.Genre
	}
	sampleRateVal := nullableInt(meta.SampleRateHz)
	bitDepthVal := nullableInt(meta.BitDepth)
	_, err = s.db.ExecContext(ctx, `
		UPDATE tracks SET album_id = ?, title = ?, title_sort = ?, artist_name = ?, track_no = ?,
			duration_ms = ?, format = ?, size_bytes = ?, file_mtime = ?, genre = COALESCE(?, genre), sample_rate_hz = ?, bit_depth = ?,
			replaygain_track_gain_db = ?, replaygain_track_peak = ?, replaygain_album_gain_db = ?, replaygain_album_peak = ?,
			missing_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		albumID, meta.Title, sortKey(meta.Title), meta.Artist, trackNo, meta.DurationMs, meta.Format, meta.SizeBytes, mtime, genreVal, sampleRateVal, bitDepthVal,
		meta.ReplayGain.TrackGainDB, meta.ReplayGain.TrackPeak, meta.ReplayGain.AlbumGainDB, meta.ReplayGain.AlbumPeak, trackID,
	)
	if err != nil {
		return err
	}
	if previousAlbumID != albumID {
		if err := s.recomputeAlbumGenres(ctx, previousAlbumID); err != nil {
			return err
		}
		if err := s.cleanupAlbumIfEmpty(ctx, previousAlbumID); err != nil {
			return err
		}
	}
	return s.recomputeAlbumGenres(ctx, albumID)
}

func (s *Store) reconcileTrackAlbum(ctx context.Context, trackID string, meta FileMetadata) error {
	var previousAlbumID string
	if err := s.db.QueryRowContext(ctx,
		`SELECT album_id FROM tracks WHERE id = ?`, trackID,
	).Scan(&previousAlbumID); err != nil {
		return err
	}

	artistID, err := s.upsertArtist(ctx, meta.AlbumArtist)
	if err != nil {
		return err
	}
	albumID, err := s.upsertAlbum(ctx, artistID, meta.Album, meta.Year, meta.CoverMime, meta.CoverData)
	if err != nil {
		return err
	}

	trackNo := nullableInt(meta.TrackNo)
	var genreVal any
	if meta.Genre != "" {
		genreVal = meta.Genre
	}
	sampleRateVal := nullableInt(meta.SampleRateHz)
	bitDepthVal := nullableInt(meta.BitDepth)
	_, err = s.db.ExecContext(ctx, `
		UPDATE tracks SET album_id = ?, title = ?, title_sort = ?, artist_name = ?, track_no = ?,
			genre = COALESCE(?, genre), sample_rate_hz = COALESCE(?, sample_rate_hz), bit_depth = COALESCE(?, bit_depth),
			replaygain_track_gain_db = ?, replaygain_track_peak = ?, replaygain_album_gain_db = ?, replaygain_album_peak = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		albumID, meta.Title, sortKey(meta.Title), meta.Artist, trackNo, genreVal, sampleRateVal, bitDepthVal,
		meta.ReplayGain.TrackGainDB, meta.ReplayGain.TrackPeak, meta.ReplayGain.AlbumGainDB, meta.ReplayGain.AlbumPeak, trackID,
	)
	if err != nil {
		return err
	}

	if err := s.recomputeAlbumGenres(ctx, albumID); err != nil {
		return err
	}
	if previousAlbumID != albumID {
		if err := s.recomputeAlbumGenres(ctx, previousAlbumID); err != nil {
			return err
		}
		if err := s.cleanupAlbumIfEmpty(ctx, previousAlbumID); err != nil {
			return err
		}
	}
	return nil
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

func (s *Store) upsertAlbum(ctx context.Context, artistID, title string, year int, coverMime string, coverData []byte) (string, error) {
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
		if len(coverData) > 0 {
			_, _ = s.db.ExecContext(ctx, `
				UPDATE albums SET cover_mime = ?, cover_data = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ? AND (cover_data IS NULL OR length(cover_data) = 0)`,
				coverMime, coverData, id,
			)
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
	var coverMimeVal any
	var coverDataVal any
	if len(coverData) > 0 {
		coverMimeVal = coverMime
		coverDataVal = coverData
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO albums (id, artist_id, title, title_sort, year, genres, cover_mime, cover_data) VALUES (?, ?, ?, ?, ?, '[]', ?, ?)`,
		id, artistID, title, sort, yearVal, coverMimeVal, coverDataVal,
	)
	return id, err
}

func (s *Store) setAlbumCoverByTrackIfMissing(ctx context.Context, trackID, coverMime string, coverData []byte) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE albums SET cover_mime = ?, cover_data = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = (SELECT album_id FROM tracks WHERE id = ?)
		  AND (cover_data IS NULL OR length(cover_data) = 0)`,
		coverMime, coverData, trackID,
	)
	return err
}

func (s *Store) updateTrackDurationIfZero(ctx context.Context, trackID string, durationMs int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE tracks SET duration_ms = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND duration_ms = 0`,
		durationMs, trackID,
	)
	return err
}

func (s *Store) updateTrackAudioFormatIfZero(ctx context.Context, trackID string, meta FileMetadata) error {
	sampleRateVal := nullableInt(meta.SampleRateHz)
	bitDepthVal := nullableInt(meta.BitDepth)
	if sampleRateVal == nil && bitDepthVal == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE tracks SET
			sample_rate_hz = COALESCE(?, sample_rate_hz),
			bit_depth = COALESCE(?, bit_depth),
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND (sample_rate_hz IS NULL OR sample_rate_hz = 0 OR bit_depth IS NULL OR bit_depth = 0)`,
		sampleRateVal, bitDepthVal, trackID,
	)
	return err
}

func (s *Store) GetAlbumCover(ctx context.Context, albumID string) (mime string, data []byte, err error) {
	var artworkPath string
	err = s.db.QueryRowContext(ctx,
		`SELECT media_type, file_path FROM album_artwork WHERE album_id = ?`, albumID,
	).Scan(&mime, &artworkPath)
	if err == nil {
		data, err = os.ReadFile(artworkPath)
		if err != nil {
			return "", nil, fmt.Errorf("read Album Artwork %q: %w", albumID, err)
		}
		return mime, data, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", nil, fmt.Errorf("get Album Artwork %q: %w", albumID, err)
	}

	var coverMime sql.NullString
	err = s.db.QueryRowContext(ctx,
		`SELECT cover_mime, cover_data FROM albums WHERE id = ?`, albumID,
	).Scan(&coverMime, &data)
	if err == sql.ErrNoRows {
		return "", nil, ErrNotFound
	}
	if err != nil {
		return "", nil, err
	}
	if len(data) == 0 {
		return "", nil, ErrNotFound
	}
	mime = coverMime.String
	if mime == "" {
		mime = "image/jpeg"
	}
	return mime, data, nil
}

func (s *Store) BeginScan(ctx context.Context) (string, error) {
	return runStoreMutation(ctx, s, "scan presence reset", func(store *Store, tx *sql.Tx) (string, error) {
		jobID, err := store.beginScan(ctx)
		if err != nil {
			return "", err
		}
		albumIDs, err := store.listAlbumIDs(ctx)
		if err != nil {
			return "", fmt.Errorf("list reset Track Albums: %w", err)
		}
		if err := store.synchronizeLegacyAlbums(ctx, tx, albumIDs); err != nil {
			return "", err
		}
		return jobID, nil
	})
}

func (s *Store) beginScan(ctx context.Context) (string, error) {
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

func (s *Store) RecoverInterruptedScans(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE scan_jobs
		SET status = 'failed', finished_at = ?, error_message = ?
		WHERE status = 'running'`, time.Now().UTC(), INTERRUPTED_SCAN_ERROR)
	if err != nil {
		return fmt.Errorf("recover interrupted scans: %w", err)
	}
	return nil
}

func (s *Store) resetScanPresence(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tracks SET identity_key = NULL, missing_at = NULL WHERE missing_at IS NOT NULL`)
	return err
}

func (s *Store) markTrackPresent(ctx context.Context, trackID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tracks SET identity_key = NULL, missing_at = NULL WHERE id = ?`, trackID)
	return err
}

func (s *Store) MarkSeenPaths(ctx context.Context, paths map[string]struct{}) (removed int, err error) {
	return runStoreMutation(ctx, s, "missing Track reconciliation", func(store *Store, tx *sql.Tx) (int, error) {
		missingTrackIDs, listErr := store.listMissingTrackIDs(ctx, paths)
		if listErr != nil {
			return 0, listErr
		}
		albumIDs, listErr := store.listTrackAlbumIDs(ctx, missingTrackIDs)
		if listErr != nil {
			return 0, listErr
		}
		removed, markErr := store.markTracksMissing(ctx, missingTrackIDs)
		if markErr != nil {
			return removed, markErr
		}
		if syncErr := store.synchronizeLegacyAlbums(ctx, tx, albumIDs); syncErr != nil {
			return removed, syncErr
		}
		return removed, nil
	})
}

func (s *Store) synchronizeLegacyAlbums(ctx context.Context, tx *sql.Tx, albumIDs []string) error {
	for _, albumID := range albumIDs {
		if err := s.recomputeAlbumGenres(ctx, albumID); err != nil {
			return fmt.Errorf("recompute legacy Album %q Genres: %w", albumID, err)
		}
		if err := internaldb.SynchronizeLegacyAlbum(ctx, tx, albumID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) listTrackAlbumIDs(ctx context.Context, trackIDs []string) ([]string, error) {
	albumIDs := make([]string, 0, len(trackIDs))
	seenAlbumIDs := make(map[string]struct{}, len(trackIDs))
	for _, trackID := range trackIDs {
		var albumID string
		if err := s.db.QueryRowContext(ctx, `SELECT album_id FROM tracks WHERE id = ?`, trackID).Scan(&albumID); err != nil {
			return nil, fmt.Errorf("lookup Track %q Album: %w", trackID, err)
		}
		if _, exists := seenAlbumIDs[albumID]; exists {
			continue
		}
		seenAlbumIDs[albumID] = struct{}{}
		albumIDs = append(albumIDs, albumID)
	}
	return albumIDs, nil
}

func (s *Store) listMissingTrackIDs(ctx context.Context, paths map[string]struct{}) (trackIDs []string, err error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tracks.id, COALESCE(track_sources.file_path, tracks.file_path)
		FROM visible_tracks tracks
		LEFT JOIN track_sources ON track_sources.track_id = tracks.id
		WHERE COALESCE(track_sources.source_kind, 'legacy') = 'legacy'`)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()

	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		if _, ok := paths[path]; !ok {
			trackIDs = append(trackIDs, id)
		}
	}
	return trackIDs, rows.Err()
}

func (s *Store) markTracksMissing(ctx context.Context, trackIDs []string) (removed int, err error) {
	for _, trackID := range trackIDs {
		if _, err := s.db.ExecContext(ctx, `UPDATE tracks SET missing_at = CURRENT_TIMESTAMP WHERE id = ?`, trackID); err != nil {
			return removed, fmt.Errorf("mark track %q missing: %w", trackID, err)
		}
		removed++
	}
	return removed, nil
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

func nullableInt(v int) any {
	if v <= 0 {
		return nil
	}
	return v
}

func albumListOrderBy(artistID string) string {
	if artistID != "" {
		return "al.year IS NULL, al.year DESC, al.title_sort"
	}
	return "al.title_sort"
}

package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
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

func (s *Store) GetAlbumCover(ctx context.Context, albumID string) (mime string, data []byte, err error) {
	var artworkPath string
	err = s.db.QueryRowContext(ctx,
		`SELECT media_type, file_path FROM visible_album_artwork WHERE album_id = ?`, albumID,
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

func albumListOrderBy(artistID string) string {
	if artistID != "" {
		return "al.year IS NULL, al.year DESC, al.title_sort"
	}
	return "al.title_sort"
}

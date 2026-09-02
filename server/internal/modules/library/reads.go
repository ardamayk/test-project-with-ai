package library

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const activeArtistAlbumsCTE = `WITH active_artist_albums AS (
	SELECT album_artists.artist_id, album_artists.album_id
	FROM album_artists
	WHERE EXISTS (
		SELECT 1 FROM tracks
		WHERE tracks.album_id = album_artists.album_id AND tracks.missing_at IS NULL AND tracks.is_pending_commit = 0
	)
	UNION
	SELECT track_artists.artist_id, tracks.album_id
	FROM track_artists
	INNER JOIN tracks ON tracks.id = track_artists.track_id
	WHERE tracks.missing_at IS NULL AND tracks.is_pending_commit = 0
)
`

const trackReadSelect = `SELECT
	t.id, t.title, t.artist_name, t.album_id, al.title,
	t.track_no, COALESCE(t.disc_no, 1), t.track_total, t.disc_total,
	t.duration_ms, COALESCE(ts.source_format, t.format), COALESCE(ts.size_bytes, t.size_bytes),
	COALESCE(t.genre, ''), COALESCE(t.sample_rate_hz, 0), COALESCE(t.bit_depth, 0),
	COALESCE(t.channel_count, 0), COALESCE(t.bitrate_bps, 0), t.codec, t.container,
	t.sample_format, COALESCE(ts.file_path, t.file_path), t.replaygain_track_gain_db,
	t.replaygain_track_peak, t.replaygain_album_gain_db, t.replaygain_album_peak
FROM tracks t
INNER JOIN albums al ON al.id = t.album_id
LEFT JOIN track_sources ts ON ts.track_id = t.id
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanExpandedTrack(scanner rowScanner) (Track, error) {
	var track Track
	var trackNo, trackTotal, discTotal sql.NullInt64
	var codec, container, sampleFormat sql.NullString
	err := scanner.Scan(
		&track.ID, &track.Title, &track.ArtistName, &track.AlbumID, &track.AlbumTitle,
		&trackNo, &track.DiscNo, &trackTotal, &discTotal, &track.DurationMs, &track.Format,
		&track.SizeBytes, &track.Genre, &track.SampleRateHz, &track.BitDepth, &track.ChannelCount,
		&track.BitrateBps, &codec, &container, &sampleFormat, &track.FilePath,
		&track.ReplayGain.TrackGainDB, &track.ReplayGain.TrackPeak,
		&track.ReplayGain.AlbumGainDB, &track.ReplayGain.AlbumPeak,
	)
	if err != nil {
		return Track{}, err
	}
	track.TrackNo = optionalInt(trackNo)
	track.TrackTotal = optionalInt(trackTotal)
	track.DiscTotal = optionalInt(discTotal)
	track.Codec = codec.String
	track.Container = container.String
	track.SampleFormat = sampleFormat.String
	if track.BitrateBps > 0 {
		track.BitrateKbps = track.BitrateBps / 1000
	} else {
		enrichTrackBitrate(&track)
	}
	return track, nil
}

func optionalInt(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}

func setAlbumDates(album *Album, year sql.NullInt64, releaseDate sql.NullString) {
	if year.Valid {
		value := int(year.Int64)
		album.Year = &value
	}
	if releaseDate.Valid {
		value := releaseDate.String
		album.ReleaseDate = &value
	}
}

func (s *Store) enrichAlbums(ctx context.Context, albums []Album) error {
	albumIDs := albumIDs(albums)
	credits, err := s.readAlbumArtists(ctx, albumIDs)
	if err != nil {
		return fmt.Errorf("enrich Albums with Artist credits: %w", err)
	}
	genres, err := s.readAlbumGenres(ctx, albumIDs)
	if err != nil {
		return fmt.Errorf("enrich Albums with Genres: %w", err)
	}
	releaseIdentifiers, err := s.readAlbumReleaseIdentifiers(ctx, albumIDs)
	if err != nil {
		return fmt.Errorf("enrich Albums with release identifiers: %w", err)
	}
	artwork, err := s.readAlbumArtwork(ctx, albumIDs)
	if err != nil {
		return fmt.Errorf("enrich Albums with Artwork: %w", err)
	}
	for index := range albums {
		album := &albums[index]
		album.AlbumArtists = credits[album.ID]
		if len(album.AlbumArtists) == 0 {
			album.AlbumArtists = []ArtistCredit{{ID: album.ArtistID, Name: album.ArtistName}}
		}
		album.ArtistID = album.AlbumArtists[0].ID
		album.ArtistName = album.AlbumArtists[0].Name
		album.GenreItems = genres[album.ID]
		if album.GenreItems == nil {
			album.GenreItems = []Genre{}
		}
		album.ReleaseIdentifiers = releaseIdentifiers[album.ID]
		if album.ReleaseIdentifiers == nil {
			album.ReleaseIdentifiers = []ReleaseIdentifier{}
		}
		album.Artwork = artwork[album.ID]
	}
	return nil
}

func (s *Store) enrichTracks(ctx context.Context, tracks []Track) error {
	trackIDs := trackIDs(tracks)
	artists, err := s.readTrackArtists(ctx, trackIDs)
	if err != nil {
		return fmt.Errorf("enrich Tracks with Artist credits: %w", err)
	}
	genres, err := s.readTrackGenres(ctx, trackIDs)
	if err != nil {
		return fmt.Errorf("enrich Tracks with Genres: %w", err)
	}
	for index := range tracks {
		track := &tracks[index]
		track.Artists = artists[track.ID]
		if len(track.Artists) == 0 {
			track.Artists = []ArtistCredit{{Name: track.ArtistName}}
		}
		track.Genres = genres[track.ID]
		if track.Genres == nil {
			track.Genres = []Genre{}
		}
	}
	return nil
}

func (s *Store) readAlbumArtists(ctx context.Context, albumIDs []string) (map[string][]ArtistCredit, error) {
	return readRelations(ctx, s, "read Album Artists", `SELECT credits.album_id, artists.id, artists.name
		FROM album_artists credits
		INNER JOIN artists ON artists.id = credits.artist_id
		WHERE credits.album_id IN (%s)
		ORDER BY credits.album_id, credits.position`, albumIDs, func(rows *sql.Rows) (string, ArtistCredit, error) {
		var albumID string
		var credit ArtistCredit
		err := rows.Scan(&albumID, &credit.ID, &credit.Name)
		return albumID, credit, err
	})
}

func (s *Store) readTrackArtists(ctx context.Context, trackIDs []string) (map[string][]ArtistCredit, error) {
	return readRelations(ctx, s, "read Track Artists", `SELECT credits.track_id, artists.id, artists.name
		FROM track_artists credits
		INNER JOIN artists ON artists.id = credits.artist_id
		WHERE credits.track_id IN (%s)
		ORDER BY credits.track_id, credits.position`, trackIDs, func(rows *sql.Rows) (string, ArtistCredit, error) {
		var trackID string
		var credit ArtistCredit
		err := rows.Scan(&trackID, &credit.ID, &credit.Name)
		return trackID, credit, err
	})
}

func (s *Store) readAlbumGenres(ctx context.Context, albumIDs []string) (map[string][]Genre, error) {
	return readRelations(ctx, s, "read Album Genres", `SELECT tracks.album_id, genres.id, genres.name, MIN(relations.position)
		FROM tracks
		INNER JOIN track_genres relations ON relations.track_id = tracks.id
		INNER JOIN genres ON genres.id = relations.genre_id
		WHERE tracks.missing_at IS NULL AND tracks.is_pending_commit = 0 AND tracks.album_id IN (%s)
		GROUP BY tracks.album_id, genres.id, genres.name
		ORDER BY tracks.album_id, MIN(relations.position), genres.name`, albumIDs, func(rows *sql.Rows) (string, Genre, error) {
		var albumID string
		var genre Genre
		var position int
		err := rows.Scan(&albumID, &genre.ID, &genre.Name, &position)
		return albumID, genre, err
	})
}

func (s *Store) readTrackGenres(ctx context.Context, trackIDs []string) (map[string][]Genre, error) {
	return readRelations(ctx, s, "read Track Genres", `SELECT relations.track_id, genres.id, genres.name
		FROM track_genres relations
		INNER JOIN genres ON genres.id = relations.genre_id
		WHERE relations.track_id IN (%s)
		ORDER BY relations.track_id, relations.position`, trackIDs, func(rows *sql.Rows) (string, Genre, error) {
		var trackID string
		var genre Genre
		err := rows.Scan(&trackID, &genre.ID, &genre.Name)
		return trackID, genre, err
	})
}

func (s *Store) readAlbumReleaseIdentifiers(ctx context.Context, albumIDs []string) (map[string][]ReleaseIdentifier, error) {
	return readRelations(ctx, s, "read Album release identifiers", `SELECT album_id, scheme, value
		FROM album_release_identifiers
		WHERE album_id IN (%s)
		ORDER BY album_id, scheme`, albumIDs, func(rows *sql.Rows) (string, ReleaseIdentifier, error) {
		var albumID string
		var identifier ReleaseIdentifier
		err := rows.Scan(&albumID, &identifier.Scheme, &identifier.Value)
		return albumID, identifier, err
	})
}

func readRelations[T any](
	ctx context.Context,
	store *Store,
	operation string,
	template string,
	ids []string,
	scan func(*sql.Rows) (string, T, error),
) (map[string][]T, error) {
	result := make(map[string][]T, len(ids))
	query, args := relationQuery(template, ids)
	if query == "" {
		return result, nil
	}
	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s for IDs %q: %w", operation, ids, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		ownerID, value, scanErr := scan(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan %s for IDs %q: %w", operation, ids, scanErr)
		}
		result[ownerID] = append(result[ownerID], value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s for IDs %q: %w", operation, ids, err)
	}
	return result, nil
}

func (s *Store) readAlbumArtwork(ctx context.Context, albumIDs []string) (map[string]*AlbumArtworkMetadata, error) {
	result := make(map[string]*AlbumArtworkMetadata, len(albumIDs))
	placeholders, args := queryPlaceholders(albumIDs)
	if placeholders == "" {
		return result, nil
	}
	query := fmt.Sprintf(`SELECT album_id, source_track_id, content_sha256, media_type,
		width, height, encoded_size_bytes
		FROM album_artwork WHERE album_id IN (%s)
		UNION ALL
		SELECT legacy.album_id, legacy.source_track_id, legacy.content_sha256, legacy.media_type,
			legacy.width, legacy.height, legacy.encoded_size_bytes
		FROM legacy_album_artwork_metadata legacy
		WHERE legacy.album_id IN (%s) AND NOT EXISTS (
			SELECT 1 FROM album_artwork current WHERE current.album_id = legacy.album_id
		)`, placeholders, placeholders)
	args = append(args, args...)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read Album Artwork for IDs %q: %w", albumIDs, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var albumID string
		var sourceTrackID, mediaType sql.NullString
		var width, height sql.NullInt64
		artwork := &AlbumArtworkMetadata{}
		if err := rows.Scan(&albumID, &sourceTrackID, &artwork.ContentSHA256, &mediaType, &width, &height, &artwork.SizeBytes); err != nil {
			return nil, fmt.Errorf("scan Album Artwork for IDs %q: %w", albumIDs, err)
		}
		artwork.SourceTrackID = sourceTrackID.String
		artwork.MediaType = mediaType.String
		artwork.Width = int(width.Int64)
		artwork.Height = int(height.Int64)
		result[albumID] = artwork
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Album Artwork for IDs %q: %w", albumIDs, err)
	}
	return result, nil
}

func relationQuery(template string, ids []string) (string, []any) {
	placeholders, args := queryPlaceholders(ids)
	if placeholders == "" {
		return "", nil
	}
	return fmt.Sprintf(template, placeholders), args
}

func queryPlaceholders(ids []string) (string, []any) {
	if len(ids) == 0 {
		return "", nil
	}
	args := make([]any, len(ids))
	for index, id := range ids {
		args[index] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}

func albumIDs(albums []Album) []string {
	ids := make([]string, len(albums))
	for index, album := range albums {
		ids[index] = album.ID
	}
	return ids
}

func trackIDs(tracks []Track) []string {
	ids := make([]string, len(tracks))
	for index, track := range tracks {
		ids[index] = track.ID
	}
	return ids
}

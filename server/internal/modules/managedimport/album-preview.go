package managedimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
)

type AlbumPreview struct {
	Key            string          `json:"key"`
	Title          string          `json:"title"`
	AlbumArtists   []string        `json:"albumArtists"`
	ExistingAlbums []AlbumMatch    `json:"existingAlbums"`
	Artworks       []ArtworkOption `json:"artworks"`
}

type AlbumMatch struct {
	ID          string               `json:"id"`
	Year        int                  `json:"year,omitempty"`
	HasArtwork  bool                 `json:"hasArtwork"`
	Tracks      []DuplicateCandidate `json:"tracks"`
	IdentityKey string               `json:"-"`
}

type ArtworkOption struct {
	ID            string `json:"id"`
	JobID         string `json:"jobId,omitempty"`
	MediaType     string `json:"mediaType"`
	ContentSHA256 string `json:"contentSha256"`
}

type AlbumDecision struct {
	AlbumKey       string `json:"albumKey"`
	AlbumID        string `json:"albumId,omitempty"`
	CreateSeparate bool   `json:"createSeparate"`
	ArtworkID      string `json:"artworkId,omitempty"`
	ArtworkMode    string `json:"artworkMode"`
}

type importPlan struct {
	TargetRevision int             `json:"targetRevision,omitempty"`
	SkipExact      bool            `json:"skipExact,omitempty"`
	AlbumKey       string          `json:"albumKey"`
	ArtworkID      string          `json:"artworkId,omitempty"`
	Action         DuplicateAction `json:"action,omitempty"`
	TargetID       string          `json:"targetId,omitempty"`
}

func albumPreviewKey(file PreviewFile) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(previewAlbumKey(file))))
}

func (service *Service) populateAlbumPreviews(ctx context.Context, batch *Batch) error {
	batch.Albums = []AlbumPreview{}
	seen := map[string]bool{}
	for _, file := range batch.Files {
		if file.Preview == nil || file.Status != STATUS_AWAITING_CONFIRMATION {
			continue
		}
		key := albumPreviewKey(file.Preview.File)
		if seen[key] {
			continue
		}
		seen[key] = true
		album, err := service.store.albumPreview(ctx, batch.ID, file.Preview.File)
		if err != nil {
			return err
		}
		batch.Albums = append(batch.Albums, album)
	}
	return nil
}

func (store *Store) albumPreview(ctx context.Context, batchID string, file PreviewFile) (AlbumPreview, error) {
	album := AlbumPreview{Key: albumPreviewKey(file), Title: file.Album, AlbumArtists: file.AlbumArtists}
	var err error
	album.ExistingAlbums, err = store.matchAlbums(ctx, previewAlbumKey(file))
	if err != nil {
		return album, err
	}
	album.Artworks, err = store.listArtworkOptions(ctx, batchID, album.Key)
	return album, err
}

func (store *Store) matchAlbums(ctx context.Context, key string) (matches []AlbumMatch, returnErr error) {
	prefix := key + "\x1fmanaged-import-edition:"
	rows, err := store.database.QueryContext(ctx, `SELECT id, identity_key, COALESCE(year, 0),
        EXISTS(SELECT 1 FROM album_artwork WHERE album_id = albums.id)
        FROM albums WHERE identity_key = ? OR substr(identity_key, 1, length(?)) = ? ORDER BY identity_key, id`, key, prefix, prefix)
	if err != nil {
		return nil, fmt.Errorf("match import Albums: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	matches = []AlbumMatch{}
	for rows.Next() {
		var album AlbumMatch
		if err = rows.Scan(&album.ID, &album.IdentityKey, &album.Year, &album.HasArtwork); err != nil {
			return nil, err
		}
		matches = append(matches, album)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	for index := range matches {
		matches[index].Tracks, err = store.albumTracks(ctx, matches[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return matches, nil
}

func (store *Store) albumTracks(ctx context.Context, albumID string) ([]DuplicateCandidate, error) {
	rows, err := store.database.QueryContext(ctx, `SELECT id FROM visible_tracks WHERE album_id = ? ORDER BY disc_no, track_no, id`, albumID)
	if err != nil {
		return nil, fmt.Errorf("list import Album tracks: %w", err)
	}
	ids, err := scanTrackIDs(rows, "Album tracks")
	if err != nil {
		return nil, err
	}
	return store.readDuplicateCandidates(ctx, ids)
}

func (store *Store) saveArtwork(ctx context.Context, batchID, id, jobID, albumKey string, artwork library.AlbumArtwork) error {
	result, err := store.database.ExecContext(ctx, `INSERT INTO managed_import_artwork
        (batch_id, id, job_id, album_key, media_type, width, height, content_sha256, data)
        SELECT id, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ? FROM managed_import_batches WHERE id = ? AND status = 'uploading'
        ON CONFLICT(batch_id, id) DO UPDATE SET media_type = excluded.media_type, width = excluded.width,
        height = excluded.height, content_sha256 = excluded.content_sha256, data = excluded.data`,
		id, jobID, albumKey, artwork.MIMEType, artwork.Width, artwork.Height, artwork.SHA256, artwork.Data, batchID)
	if err != nil {
		return fmt.Errorf("stage import Album artwork: %w", err)
	}
	return requireMutation(result)
}

func (store *Store) listArtworkOptions(ctx context.Context, batchID, albumKey string) (options []ArtworkOption, returnErr error) {
	rows, err := store.database.QueryContext(ctx, `SELECT id, COALESCE(job_id, ''), media_type, content_sha256
        FROM managed_import_artwork WHERE batch_id = ? AND album_key = ? ORDER BY (job_id IS NULL) DESC, id`, batchID, albumKey)
	if err != nil {
		return nil, fmt.Errorf("list import artwork: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	options = []ArtworkOption{}
	seen := map[string]bool{}
	for rows.Next() {
		var option ArtworkOption
		if err := rows.Scan(&option.ID, &option.JobID, &option.MediaType, &option.ContentSHA256); err != nil {
			return nil, err
		}
		if seen[option.ContentSHA256] {
			continue
		}
		seen[option.ContentSHA256] = true
		options = append(options, option)
	}
	return options, rows.Err()
}

func (store *Store) loadArtwork(ctx context.Context, batchID, id string) (library.AlbumArtwork, error) {
	var artwork library.AlbumArtwork
	err := store.database.QueryRowContext(ctx, `SELECT media_type, width, height, content_sha256, data
        FROM managed_import_artwork WHERE batch_id = ? AND id = ?`, batchID, id).
		Scan(&artwork.MIMEType, &artwork.Width, &artwork.Height, &artwork.SHA256, &artwork.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return artwork, ErrNotFound
	}
	if err != nil {
		return artwork, fmt.Errorf("load staged import artwork: %w", err)
	}
	return artwork, nil
}

func decodeImportPlan(job importJob) (importPlan, error) {
	var plan importPlan
	if job.ImportPlanJSON == "" {
		return plan, nil
	}
	if err := json.Unmarshal([]byte(job.ImportPlanJSON), &plan); err != nil {
		return plan, fmt.Errorf("decode import plan for %q: %w", job.ID, err)
	}
	return plan, nil
}

func (service *Service) applyImportPlan(ctx context.Context, job importJob, inspection library.MediaInspection) (library.MediaInspection, error) {
	plan, err := decodeImportPlan(job)
	if err != nil {
		return inspection, err
	}
	if plan.AlbumKey != "" {
		inspection.Metadata.AlbumIdentityKey = plan.AlbumKey
		inspection.AlbumArtwork = library.AlbumArtwork{}
		if plan.ArtworkID != "" {
			inspection.AlbumArtwork, err = service.store.loadArtwork(ctx, job.BatchID, plan.ArtworkID)
		}
		if err != nil {
			return inspection, err
		}
	}
	var artwork library.AlbumArtwork
	err = service.store.database.QueryRowContext(ctx, `SELECT content_sha256, media_type, width, height FROM album_artwork
        JOIN albums ON albums.id = album_artwork.album_id WHERE albums.identity_key = ?`, albumIdentityKey(inspection.Metadata)).
		Scan(&artwork.SHA256, &artwork.MIMEType, &artwork.Width, &artwork.Height)
	if err == nil {
		inspection.AlbumArtwork = artwork
		return inspection, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return inspection, nil
	}
	return inspection, fmt.Errorf("read existing Album artwork: %w", err)
}

func (store *Store) populateCandidateMetadata(ctx context.Context, candidate *DuplicateCandidate) error {
	target, err := readReplacementTargetRow(ctx, store.database, candidate.TrackID)
	if errors.Is(err, ErrTrackNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	genres, err := queryOrderedNames(ctx, store.database, `SELECT genres.name FROM track_genres JOIN genres ON genres.id = track_genres.genre_id WHERE track_id = ? ORDER BY position`, candidate.TrackID)
	if err != nil {
		return err
	}
	artists, err := queryOrderedNames(ctx, store.database, `SELECT artists.name FROM album_artists JOIN artists ON artists.id = album_artists.artist_id WHERE album_id = ? ORDER BY position`, target.AlbumID)
	if err != nil {
		return err
	}
	candidate.CurrentFile = &PreviewFile{OriginalFilename: target.Title + "." + target.Format, Title: target.Title, TitleKey: candidate.TitleKey, Artists: candidate.Artists, AlbumArtists: artists, Album: target.Album, Genres: genres, Year: target.Year, TrackNo: target.TrackNo, TrackTotal: target.TrackTotal, DiscNo: target.DiscNo, DiscTotal: target.DiscTotal, Format: target.Format, Codec: target.Codec, Container: target.Container, DurationMs: target.DurationMs, SizeBytes: target.SizeBytes, SampleRateHz: target.SampleRateHz, ChannelCount: target.ChannelCount, BitDepth: target.BitDepth, BitrateKbps: target.BitrateKbps, ArtworkMediaType: target.ArtworkType}
	return nil
}

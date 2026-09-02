package managedimport

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
)

const PERMANENT_DELETE_CONFIRMATION_HEADER = "X-Permanent-Delete"

var permanentTrackDeletionMu sync.Mutex

type TrackDeletionManagedFile struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
}

type TrackDeletionPlaylistReference struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type TrackDeletionQueueReference struct {
	UserID    string `json:"userId"`
	ItemCount int    `json:"itemCount"`
}

type TrackDeletionPreview struct {
	TrackID            string                           `json:"trackId"`
	TrackTitle         string                           `json:"trackTitle"`
	ManagedFile        TrackDeletionManagedFile         `json:"managedFile"`
	PlaylistReferences []TrackDeletionPlaylistReference `json:"playlistReferences"`
	QueueReferences    []TrackDeletionQueueReference    `json:"queueReferences"`
	ConfirmationToken  string                           `json:"confirmationToken"`
}

type TrackDeletionConfirmation struct {
	ConfirmationToken string `json:"confirmationToken"`
}

type TrackDeletionResult struct {
	DeletedFiles int `json:"deletedFiles"`
}

type trackDeletionState struct {
	Preview        TrackDeletionPreview
	AlbumID        string
	FilePath       string
	ContentSHA256  string
	TrackRevision  int
	SourceRevision int
}

type deletionQueryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (service *Service) PreviewTrackDeletion(ctx context.Context, trackID string) (TrackDeletionPreview, error) {
	state, err := loadTrackDeletionState(ctx, service.store.database, service.storage, trackID)
	if err != nil {
		return TrackDeletionPreview{}, err
	}
	return state.Preview, nil
}

func (service *Service) DeleteTrack(ctx context.Context, confirmation TrackDeletionRequest) (TrackDeletionResult, error) {
	permanentTrackDeletionMu.Lock()
	defer permanentTrackDeletionMu.Unlock()
	managedImportCommitMu.Lock()
	defer managedImportCommitMu.Unlock()

	state, err := service.prepareTrackDeletion(ctx, confirmation)
	if err != nil {
		return TrackDeletionResult{}, err
	}
	isRemoved, removeErr := service.storage.RemoveManagedFile(state.FilePath, state.ContentSHA256)
	if removeErr != nil && !isRemoved {
		return TrackDeletionResult{}, errors.Join(removeErr, service.cancelPreparedTrackDeletion(ctx, state.Preview.TrackID))
	}
	if finalizeErr := service.finalizePreparedTrackDeletion(ctx, state.Preview.TrackID); finalizeErr != nil {
		return TrackDeletionResult{}, errors.Join(removeErr, finalizeErr)
	}
	return TrackDeletionResult{DeletedFiles: 1}, removeErr
}

type TrackDeletionRequest struct {
	TrackID           string
	ConfirmationToken string
}

func (service *Service) prepareTrackDeletion(ctx context.Context, confirmation TrackDeletionRequest) (state trackDeletionState, returnErr error) {
	transaction, err := service.store.database.BeginTx(ctx, nil)
	if err != nil {
		return state, fmt.Errorf("begin Permanent Track Deletion preparation: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rollbackDeletionTransaction(transaction, "preparation")) }()
	state, err = loadTrackDeletionState(ctx, transaction, service.storage, confirmation.TrackID)
	if err != nil {
		return state, err
	}
	if !tokensEqual(state.Preview.ConfirmationToken, confirmation.ConfirmationToken) {
		return state, ErrDeletionConflict
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO permanent_track_deletions (track_id, file_path, content_sha256)
		VALUES (?, ?, ?)`, confirmation.TrackID, state.FilePath, state.ContentSHA256); err != nil {
		return state, fmt.Errorf("journal Permanent Track Deletion: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE tracks SET missing_at = CURRENT_TIMESTAMP WHERE id = ?`, confirmation.TrackID); err != nil {
		return state, fmt.Errorf("hide pending Permanent Track Deletion: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return state, fmt.Errorf("commit Permanent Track Deletion preparation: %w", err)
	}
	return state, nil
}

func (service *Service) cancelPreparedTrackDeletion(ctx context.Context, trackID string) (returnErr error) {
	transaction, err := service.store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Permanent Track Deletion cancellation: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rollbackDeletionTransaction(transaction, "cancellation")) }()
	if _, err := transaction.ExecContext(ctx, `UPDATE tracks SET missing_at = NULL WHERE id = ?`, trackID); err != nil {
		return fmt.Errorf("restore Managed Track after failed deletion: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM permanent_track_deletions WHERE track_id = ?`, trackID); err != nil {
		return fmt.Errorf("clear failed Permanent Track Deletion journal: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit Permanent Track Deletion cancellation: %w", err)
	}
	return nil
}

func (service *Service) finalizePreparedTrackDeletion(ctx context.Context, trackID string) (returnErr error) {
	transaction, err := service.store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Permanent Track Deletion finalization: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rollbackDeletionTransaction(transaction, "finalization")) }()
	state, err := loadPendingTrackDeletionState(ctx, transaction, trackID)
	if err != nil {
		return err
	}
	if err := deleteTrackRelationships(ctx, transaction, state); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit Permanent Track Deletion finalization: %w", err)
	}
	return nil
}

func (service *Service) RecoverPendingTrackDeletions(ctx context.Context) (returnErr error) {
	rows, err := service.store.database.QueryContext(ctx, `SELECT track_id, file_path, content_sha256 FROM permanent_track_deletions ORDER BY created_at, track_id`)
	if err != nil {
		return fmt.Errorf("list pending Permanent Track Deletions: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, closeDeletionRows(rows, "pending Permanent Track Deletions"))
	}()
	type pendingDeletion struct{ trackID, filePath, contentSHA256 string }
	pending := []pendingDeletion{}
	for rows.Next() {
		var deletion pendingDeletion
		if err := rows.Scan(&deletion.trackID, &deletion.filePath, &deletion.contentSHA256); err != nil {
			return fmt.Errorf("read pending Permanent Track Deletion: %w", err)
		}
		pending = append(pending, deletion)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate pending Permanent Track Deletions: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close pending Permanent Track Deletions before recovery: %w", err)
	}
	for _, deletion := range pending {
		isRemoved, removeErr := service.storage.RemoveManagedFile(deletion.filePath, deletion.contentSHA256)
		if removeErr != nil && !isRemoved {
			return fmt.Errorf("recover pending Permanent Track Deletion %q: %w", deletion.trackID, removeErr)
		}
		if finalizeErr := service.finalizePreparedTrackDeletion(ctx, deletion.trackID); finalizeErr != nil {
			return fmt.Errorf("finalize recovered Permanent Track Deletion %q: %w", deletion.trackID, errors.Join(removeErr, finalizeErr))
		}
		if removeErr != nil {
			return fmt.Errorf("close Managed Storage after recovering Permanent Track Deletion %q: %w", deletion.trackID, removeErr)
		}
	}
	return nil
}

func loadPendingTrackDeletionState(ctx context.Context, queryer deletionQueryer, trackID string) (trackDeletionState, error) {
	var state trackDeletionState
	err := queryer.QueryRowContext(ctx, `
		SELECT tracks.album_id, permanent_track_deletions.file_path, permanent_track_deletions.content_sha256
		FROM permanent_track_deletions
		JOIN tracks ON tracks.id = permanent_track_deletions.track_id
		WHERE tracks.id = ?`, trackID).Scan(&state.AlbumID, &state.FilePath, &state.ContentSHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return state, ErrTrackNotFound
	}
	if err != nil {
		return state, fmt.Errorf("load pending Permanent Track Deletion: %w", err)
	}
	state.Preview.TrackID = trackID
	state.Preview.QueueReferences, err = listDeletionQueues(ctx, queryer, trackID)
	return state, err
}

func rollbackDeletionTransaction(transaction *sql.Tx, operation string) error {
	err := transaction.Rollback()
	if err == nil || errors.Is(err, sql.ErrTxDone) {
		return nil
	}
	return fmt.Errorf("roll back Permanent Track Deletion %s: %w", operation, err)
}

func closeDeletionRows(rows *sql.Rows, description string) error {
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close %s: %w", description, err)
	}
	return nil
}

func loadTrackDeletionState(ctx context.Context, queryer deletionQueryer, storage *Storage, trackID string) (trackDeletionState, error) {
	var state trackDeletionState
	var sourceKind string
	var sizeBytes int64
	err := queryer.QueryRowContext(ctx, `
		SELECT tracks.title, tracks.album_id, tracks.revision,
			track_sources.source_kind, track_sources.file_path, track_sources.content_sha256,
			track_sources.size_bytes, track_sources.revision
		FROM tracks
		JOIN track_sources ON track_sources.track_id = tracks.id
		WHERE tracks.id = ? AND tracks.missing_at IS NULL`, trackID).Scan(
		&state.Preview.TrackTitle, &state.AlbumID, &state.TrackRevision,
		&sourceKind, &state.FilePath, &state.ContentSHA256, &sizeBytes, &state.SourceRevision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return trackDeletionState{}, ErrTrackNotFound
	}
	if err != nil {
		return trackDeletionState{}, fmt.Errorf("load Managed Track deletion: %w", err)
	}
	if sourceKind != "managed" {
		return trackDeletionState{}, ErrNotManagedTrack
	}
	relativePath, actualSize, err := storage.ResolveManagedFile(state.FilePath, state.ContentSHA256)
	if err != nil {
		return trackDeletionState{}, err
	}
	if sizeBytes != actualSize {
		return trackDeletionState{}, fmt.Errorf("%w: managed file size changed", ErrDeletionConflict)
	}
	state.Preview.TrackID = trackID
	state.Preview.ManagedFile = TrackDeletionManagedFile{Path: filepath.ToSlash(relativePath), SizeBytes: actualSize}
	state.Preview.PlaylistReferences, err = listDeletionPlaylists(ctx, queryer, trackID)
	if err != nil {
		return trackDeletionState{}, err
	}
	state.Preview.QueueReferences, err = listDeletionQueues(ctx, queryer, trackID)
	if err != nil {
		return trackDeletionState{}, err
	}
	state.Preview.ConfirmationToken, err = deletionToken(state)
	return state, err
}

func listDeletionPlaylists(ctx context.Context, queryer deletionQueryer, trackID string) (references []TrackDeletionPlaylistReference, returnErr error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT playlists.id, playlists.name
		FROM playlist_tracks
		JOIN playlists ON playlists.id = playlist_tracks.playlist_id
		WHERE playlist_tracks.track_id = ?
		ORDER BY playlists.name, playlists.id`, trackID)
	if err != nil {
		return nil, fmt.Errorf("list affected Playlists: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, closeDeletionRows(rows, "affected Playlists")) }()
	references = []TrackDeletionPlaylistReference{}
	for rows.Next() {
		var reference TrackDeletionPlaylistReference
		if err := rows.Scan(&reference.ID, &reference.Name); err != nil {
			return nil, fmt.Errorf("read affected Playlist: %w", err)
		}
		references = append(references, reference)
	}
	return references, rows.Err()
}

func listDeletionQueues(ctx context.Context, queryer deletionQueryer, trackID string) (references []TrackDeletionQueueReference, returnErr error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT user_id, COUNT(*)
		FROM playback_queue
		WHERE track_id = ?
		GROUP BY user_id
		ORDER BY user_id`, trackID)
	if err != nil {
		return nil, fmt.Errorf("list affected Queues: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, closeDeletionRows(rows, "affected Queues")) }()
	references = []TrackDeletionQueueReference{}
	for rows.Next() {
		var reference TrackDeletionQueueReference
		if err := rows.Scan(&reference.UserID, &reference.ItemCount); err != nil {
			return nil, fmt.Errorf("read affected Queue: %w", err)
		}
		references = append(references, reference)
	}
	return references, rows.Err()
}

func deletionToken(state trackDeletionState) (string, error) {
	payload := struct {
		TrackID            string
		TrackTitle         string
		AlbumID            string
		FilePath           string
		ContentSHA256      string
		TrackRevision      int
		SourceRevision     int
		PlaylistReferences []TrackDeletionPlaylistReference
		QueueReferences    []TrackDeletionQueueReference
	}{
		TrackID: state.Preview.TrackID, TrackTitle: state.Preview.TrackTitle, AlbumID: state.AlbumID,
		FilePath: state.FilePath, ContentSHA256: state.ContentSHA256, TrackRevision: state.TrackRevision,
		SourceRevision: state.SourceRevision, PlaylistReferences: state.Preview.PlaylistReferences,
		QueueReferences: state.Preview.QueueReferences,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode Permanent Track Deletion preview: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func tokensEqual(expected, actual string) bool {
	if len(expected) != len(actual) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func deleteTrackRelationships(ctx context.Context, transaction *sql.Tx, state trackDeletionState) error {
	artistIDs, err := deletionArtistIDs(ctx, transaction, state.Preview.TrackID, state.AlbumID)
	if err != nil {
		return err
	}
	for _, queue := range state.Preview.QueueReferences {
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO playback_queue_state (user_id, revision, event_sequence) VALUES (?, 1, 1)
			ON CONFLICT(user_id) DO UPDATE SET revision = revision + 1, event_sequence = event_sequence + 1`, queue.UserID); err != nil {
			return fmt.Errorf("advance affected Queue revision: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM playback_queue WHERE track_id = ?`, state.Preview.TrackID); err != nil {
		return fmt.Errorf("delete Queue references: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM playlist_tracks WHERE track_id = ?`, state.Preview.TrackID); err != nil {
		return fmt.Errorf("delete Playlist references: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM managed_import_jobs WHERE track_id = ? AND status = 'committed'`, state.Preview.TrackID); err != nil {
		return fmt.Errorf("archive completed Managed Import Job before Track deletion: %w", err)
	}
	if err := moveOrDeleteArtworkReference(ctx, transaction, state.Preview.TrackID, state.AlbumID); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM tracks WHERE id = ?`, state.Preview.TrackID); err != nil {
		return fmt.Errorf("delete Managed Track: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM albums WHERE id = ? AND NOT EXISTS (SELECT 1 FROM tracks WHERE album_id = ?)`, state.AlbumID, state.AlbumID); err != nil {
		return fmt.Errorf("delete empty Album: %w", err)
	}
	for _, artistID := range artistIDs {
		if _, err := transaction.ExecContext(ctx, `
			DELETE FROM artists WHERE id = ?
			AND NOT EXISTS (SELECT 1 FROM track_artists WHERE artist_id = ?)
			AND NOT EXISTS (SELECT 1 FROM album_artists WHERE artist_id = ?)`, artistID, artistID, artistID); err != nil {
			return fmt.Errorf("delete inactive Artist: %w", err)
		}
	}
	return nil
}

func moveOrDeleteArtworkReference(ctx context.Context, transaction *sql.Tx, trackID, albumID string) error {
	var replacementTrackID string
	err := transaction.QueryRowContext(ctx, `SELECT id FROM tracks WHERE album_id = ? AND id != ? ORDER BY id LIMIT 1`, albumID, trackID).Scan(&replacementTrackID)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = transaction.ExecContext(ctx, `DELETE FROM album_artwork WHERE album_id = ?`, albumID)
	} else if err == nil {
		_, err = transaction.ExecContext(ctx, `UPDATE album_artwork SET source_track_id = ? WHERE album_id = ? AND source_track_id = ?`, replacementTrackID, albumID, trackID)
	}
	if err != nil {
		return fmt.Errorf("update Album Artwork source after Track deletion: %w", err)
	}
	return nil
}

func deletionArtistIDs(ctx context.Context, transaction *sql.Tx, trackID, albumID string) (artistIDs []string, returnErr error) {
	rows, err := transaction.QueryContext(ctx, `
		SELECT artist_id FROM track_artists WHERE track_id = ?
		UNION SELECT artist_id FROM album_artists WHERE album_id = ?`, trackID, albumID)
	if err != nil {
		return nil, fmt.Errorf("list affected Artists: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, closeDeletionRows(rows, "affected Artists")) }()
	artistIDs = []string{}
	for rows.Next() {
		var artistID string
		if err := rows.Scan(&artistID); err != nil {
			return nil, err
		}
		artistIDs = append(artistIDs, artistID)
	}
	return artistIDs, rows.Err()
}

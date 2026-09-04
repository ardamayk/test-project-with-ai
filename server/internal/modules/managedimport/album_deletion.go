package managedimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// AlbumDeletionPreview is the Album-level review of one Permanent Track
// Deletion per Track. Deleting an Album is not its own operation: the preview
// aggregates the per-Track previews so the user confirms once, and the
// confirmation token binds that exact set of Tracks and references.
type AlbumDeletionPreview struct {
	AlbumID            string                           `json:"albumId"`
	AlbumTitle         string                           `json:"albumTitle"`
	TrackCount         int                              `json:"trackCount"`
	TotalSizeBytes     int64                            `json:"totalSizeBytes"`
	Tracks             []AlbumDeletionTrack             `json:"tracks"`
	PlaylistReferences []TrackDeletionPlaylistReference `json:"playlistReferences"`
	QueueReferences    []TrackDeletionQueueReference    `json:"queueReferences"`
	ConfirmationToken  string                           `json:"confirmationToken"`
}

type AlbumDeletionTrack struct {
	TrackID    string `json:"trackId"`
	TrackTitle string `json:"trackTitle"`
	SizeBytes  int64  `json:"sizeBytes"`
}

// AlbumDeletionResult reports each Track separately because every Track
// commits on its own: a failure stops the run and leaves the remaining Tracks
// untouched rather than undoing the ones already deleted. StoppedAt is nil
// when every Track was deleted.
type AlbumDeletionResult struct {
	Deleted      []AlbumDeletionTrack `json:"deleted"`
	StoppedAt    *AlbumDeletionFailed `json:"stoppedAt"`
	DeletedFiles int                  `json:"deletedFiles"`
}

type AlbumDeletionFailed struct {
	TrackID    string `json:"trackId"`
	TrackTitle string `json:"trackTitle"`
	Reason     string `json:"reason"`
}

type AlbumDeletionRequest struct {
	AlbumID           string
	ConfirmationToken string
}

type albumDeletionState struct {
	Preview AlbumDeletionPreview
	Tracks  []trackDeletionState
}

func (service *Service) PreviewAlbumDeletion(ctx context.Context, albumID string) (AlbumDeletionPreview, error) {
	state, err := loadAlbumDeletionState(ctx, service.store.database, service.storage, albumID)
	if err != nil {
		return AlbumDeletionPreview{}, err
	}
	return state.Preview, nil
}

// DeleteAlbum re-derives the preview, refuses a stale token, then runs one
// Permanent Track Deletion per Track in album order. The album token is
// checked once, before the loop; no lock spans the run. Each Track's
// deletion is protected by DeleteTrack itself, which re-derives and compares
// the Track's state inside its own transaction. The per-Track token minted
// here only feeds that check; it is re-derived right before each deletion
// because deleting a sibling changes what the next one means (for example
// which Track carries the final album artwork).
func (service *Service) DeleteAlbum(ctx context.Context, request AlbumDeletionRequest) (AlbumDeletionResult, error) {
	state, err := loadAlbumDeletionState(ctx, service.store.database, service.storage, request.AlbumID)
	if err != nil {
		return AlbumDeletionResult{}, err
	}
	if !tokensEqual(state.Preview.ConfirmationToken, request.ConfirmationToken) {
		return AlbumDeletionResult{}, fmt.Errorf("%w: album deletion preview changed", ErrDeletionConflict)
	}
	result := AlbumDeletionResult{Deleted: []AlbumDeletionTrack{}}
	for _, track := range state.Preview.Tracks {
		var deleteErr error
		var deleted TrackDeletionResult
		if service.albumDeletionTrackHook != nil {
			deleteErr = service.albumDeletionTrackHook(track.TrackID)
		}
		var trackState trackDeletionState
		if deleteErr == nil {
			trackState, deleteErr = loadTrackDeletionState(ctx, service.store.database, service.storage, track.TrackID)
		}
		if deleteErr == nil {
			deleted, deleteErr = service.DeleteTrack(ctx, TrackDeletionRequest{TrackID: track.TrackID, ConfirmationToken: trackState.Preview.ConfirmationToken})
		}
		if deleteErr != nil {
			result.StoppedAt = &AlbumDeletionFailed{TrackID: track.TrackID, TrackTitle: track.TrackTitle, Reason: deleteErr.Error()}
			break
		}
		result.Deleted = append(result.Deleted, track)
		result.DeletedFiles += deleted.DeletedFiles
	}
	return result, nil
}

func loadAlbumDeletionState(ctx context.Context, queryer deletionQueryer, storage *Storage, albumID string) (albumDeletionState, error) {
	var state albumDeletionState
	var albumRevision int
	err := queryer.QueryRowContext(ctx, `SELECT title, revision FROM albums WHERE id = ?`, albumID).Scan(&state.Preview.AlbumTitle, &albumRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return albumDeletionState{}, ErrAlbumNotFound
	}
	if err != nil {
		return albumDeletionState{}, fmt.Errorf("load Album deletion: %w", err)
	}
	trackIDs, err := listAlbumDeletionTrackIDs(ctx, queryer, albumID)
	if err != nil {
		return albumDeletionState{}, err
	}
	if len(trackIDs) == 0 {
		return albumDeletionState{}, ErrAlbumNotFound
	}
	state.Preview.AlbumID = albumID
	state.Preview.Tracks = make([]AlbumDeletionTrack, 0, len(trackIDs))
	state.Preview.PlaylistReferences = []TrackDeletionPlaylistReference{}
	state.Preview.QueueReferences = []TrackDeletionQueueReference{}
	playlistSeen := map[string]bool{}
	queueItems := map[string]int{}
	queueOrder := []string{}
	for _, trackID := range trackIDs {
		trackState, loadErr := loadTrackDeletionState(ctx, queryer, storage, trackID)
		if loadErr != nil {
			return albumDeletionState{}, loadErr
		}
		state.Tracks = append(state.Tracks, trackState)
		state.Preview.Tracks = append(state.Preview.Tracks, AlbumDeletionTrack{
			TrackID: trackID, TrackTitle: trackState.Preview.TrackTitle, SizeBytes: trackState.Preview.ManagedFile.SizeBytes,
		})
		state.Preview.TotalSizeBytes += trackState.Preview.ManagedFile.SizeBytes
		for _, playlist := range trackState.Preview.PlaylistReferences {
			if !playlistSeen[playlist.ID] {
				playlistSeen[playlist.ID] = true
				state.Preview.PlaylistReferences = append(state.Preview.PlaylistReferences, playlist)
			}
		}
		for _, queue := range trackState.Preview.QueueReferences {
			if _, known := queueItems[queue.UserID]; !known {
				queueOrder = append(queueOrder, queue.UserID)
			}
			queueItems[queue.UserID] += queue.ItemCount
		}
	}
	for _, userID := range queueOrder {
		state.Preview.QueueReferences = append(state.Preview.QueueReferences, TrackDeletionQueueReference{UserID: userID, ItemCount: queueItems[userID]})
	}
	state.Preview.TrackCount = len(state.Preview.Tracks)
	state.Preview.ConfirmationToken, err = albumDeletionToken(albumID, albumRevision, state.Tracks)
	return state, err
}

func listAlbumDeletionTrackIDs(ctx context.Context, queryer deletionQueryer, albumID string) (trackIDs []string, returnErr error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT id FROM tracks
		WHERE album_id = ? AND missing_at IS NULL AND is_pending_commit = 0
		ORDER BY COALESCE(disc_no, 1), track_no, title_sort, id`, albumID)
	if err != nil {
		return nil, fmt.Errorf("list Album Tracks for deletion: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, closeDeletionRows(rows, "Album Tracks")) }()
	for rows.Next() {
		var trackID string
		if err := rows.Scan(&trackID); err != nil {
			return nil, fmt.Errorf("read Album Track for deletion: %w", err)
		}
		trackIDs = append(trackIDs, trackID)
	}
	return trackIDs, rows.Err()
}

// albumDeletionToken binds the confirmation to the album row and to every
// per-Track token, so any change to a Track, its file, or its references
// invalidates the whole album confirmation.
func albumDeletionToken(albumID string, albumRevision int, tracks []trackDeletionState) (string, error) {
	trackTokens := make([]string, len(tracks))
	for index, track := range tracks {
		trackTokens[index] = track.Preview.ConfirmationToken
	}
	payload := struct {
		AlbumID       string
		AlbumRevision int
		TrackTokens   []string
	}{AlbumID: albumID, AlbumRevision: albumRevision, TrackTokens: trackTokens}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode Album deletion preview: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

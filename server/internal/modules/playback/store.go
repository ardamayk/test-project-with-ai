package playback

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/google/uuid"
)

type QueueItem struct {
	ID       string        `json:"id"`
	TrackID  string        `json:"trackId"`
	Position int           `json:"position"`
	Track    library.Track `json:"track"`
}

type Queue struct {
	Items         []QueueItem `json:"items"`
	Revision      string      `json:"revision"`
	EventSequence string      `json:"-"`
}

type Store struct {
	db     *sql.DB
	tracks library.TrackReader
}

func NewStore(db *sql.DB, tracks library.TrackReader) *Store {
	return &Store{db: db, tracks: tracks}
}

func (s *Store) GetQueue(ctx context.Context, userID string) (Queue, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Queue{}, fmt.Errorf("begin queue read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var revision string
	var eventSequence string
	if scanErr := tx.QueryRowContext(ctx, `
		SELECT CAST(COALESCE((
			SELECT revision FROM playback_queue_state WHERE user_id = ?
		), 0) AS TEXT), CAST(COALESCE((
			SELECT event_sequence FROM playback_queue_state WHERE user_id = ?
		), 0) AS TEXT)`, userID, userID).Scan(&revision, &eventSequence); scanErr != nil {
		return Queue{}, fmt.Errorf("get queue revision: %w", scanErr)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT pq.id, pq.track_id, pq.position
		FROM playback_queue pq
		WHERE pq.user_id = ?
		ORDER BY pq.position`, userID)
	if err != nil {
		return Queue{}, fmt.Errorf("get queue: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := []QueueItem{}
	for rows.Next() {
		var item QueueItem
		if err := rows.Scan(&item.ID, &item.TrackID, &item.Position); err != nil {
			return Queue{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return Queue{}, err
	}
	if err := rows.Close(); err != nil {
		return Queue{}, err
	}
	if err := tx.Commit(); err != nil {
		return Queue{}, err
	}

	resolvedItems := make([]QueueItem, 0, len(items))
	for _, item := range items {
		track, err := s.tracks.GetTrack(ctx, item.TrackID)
		if errors.Is(err, library.ErrNotFound) {
			continue
		}
		if err != nil {
			return Queue{}, fmt.Errorf("resolve queue track %q: %w", item.TrackID, err)
		}
		item.Track = track
		resolvedItems = append(resolvedItems, item)
	}
	return Queue{Items: resolvedItems, Revision: revision, EventSequence: eventSequence}, nil
}

func (s *Store) ReplaceQueue(ctx context.Context, userID string, trackIDs []string, expectedRevision string) (Queue, error) {
	for _, trackID := range trackIDs {
		if _, err := s.tracks.GetTrack(ctx, trackID); err != nil {
			return Queue{}, library.ErrNotFound
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Queue{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if revisionErr := advanceRevision(ctx, tx, userID, expectedRevision); revisionErr != nil {
		return Queue{}, revisionErr
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM playback_queue WHERE user_id = ?`, userID); err != nil {
		return Queue{}, err
	}

	for i, trackID := range trackIDs {
		itemID := uuid.NewString()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO playback_queue (id, user_id, position, track_id) VALUES (?, ?, ?, ?)`,
			itemID, userID, i, trackID,
		); err != nil {
			return Queue{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return Queue{}, err
	}
	return s.GetQueue(ctx, userID)
}

func (s *Store) AppendItem(ctx context.Context, userID, trackID, expectedRevision string) (Queue, error) {
	if _, err := s.tracks.GetTrack(ctx, trackID); err != nil {
		return Queue{}, library.ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Queue{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if revisionErr := advanceRevision(ctx, tx, userID, expectedRevision); revisionErr != nil {
		return Queue{}, revisionErr
	}

	var maxPos sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(position) FROM playback_queue WHERE user_id = ?`, userID,
	).Scan(&maxPos); err != nil {
		return Queue{}, err
	}

	pos := 0
	if maxPos.Valid {
		pos = int(maxPos.Int64) + 1
	}

	itemID := uuid.NewString()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO playback_queue (id, user_id, position, track_id) VALUES (?, ?, ?, ?)`,
		itemID, userID, pos, trackID,
	); err != nil {
		return Queue{}, err
	}
	if err := tx.Commit(); err != nil {
		return Queue{}, err
	}
	return s.GetQueue(ctx, userID)
}

func advanceRevision(ctx context.Context, tx *sql.Tx, userID, expectedRevision string) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO playback_queue_state (user_id, revision) VALUES (?, 0)`,
		userID,
	); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE playback_queue_state
		SET revision = revision + 1, event_sequence = event_sequence + 1
		WHERE user_id = ? AND CAST(revision AS TEXT) = ?`, userID, expectedRevision)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrRevisionConflict
	}
	return nil
}

func (s *Store) RemoveItem(ctx context.Context, userID, itemID, expectedRevision string) (Queue, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Queue{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if revisionErr := advanceRevision(ctx, tx, userID, expectedRevision); revisionErr != nil {
		return Queue{}, revisionErr
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM playback_queue WHERE id = ? AND user_id = ?`, itemID, userID,
	)
	if err != nil {
		return Queue{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Queue{}, err
	}
	if n == 0 {
		return Queue{}, ErrNotFound
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM playback_queue WHERE user_id = ? ORDER BY position`, userID)
	if err != nil {
		return Queue{}, err
	}
	itemIDs := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return Queue{}, err
		}
		itemIDs = append(itemIDs, id)
	}
	if err := rows.Err(); err != nil {
		return Queue{}, err
	}
	if err := rows.Close(); err != nil {
		return Queue{}, err
	}
	for position, id := range itemIDs {
		if _, err := tx.ExecContext(ctx,
			`UPDATE playback_queue SET position = ? WHERE id = ?`, position, id,
		); err != nil {
			return Queue{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Queue{}, err
	}

	return s.GetQueue(ctx, userID)
}

func (s *Store) ReorderItems(ctx context.Context, userID string, itemIDs []string, expectedRevision string) (Queue, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Queue{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if revisionErr := advanceRevision(ctx, tx, userID, expectedRevision); revisionErr != nil {
		return Queue{}, revisionErr
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM playback_queue WHERE user_id = ? ORDER BY position`, userID)
	if err != nil {
		return Queue{}, err
	}
	currentIDs := []string{}
	for rows.Next() {
		var itemID string
		if err := rows.Scan(&itemID); err != nil {
			_ = rows.Close()
			return Queue{}, err
		}
		currentIDs = append(currentIDs, itemID)
	}
	if err := rows.Err(); err != nil {
		return Queue{}, err
	}
	if err := rows.Close(); err != nil {
		return Queue{}, err
	}
	if !sameQueueItems(currentIDs, itemIDs) {
		return Queue{}, ErrInvalidQueueOrder
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE playback_queue SET position = -position - 1 WHERE user_id = ?`, userID,
	); err != nil {
		return Queue{}, err
	}
	for position, itemID := range itemIDs {
		if _, err := tx.ExecContext(ctx,
			`UPDATE playback_queue SET position = ? WHERE id = ? AND user_id = ?`,
			position, itemID, userID,
		); err != nil {
			return Queue{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Queue{}, err
	}
	return s.GetQueue(ctx, userID)
}

func sameQueueItems(currentIDs, requestedIDs []string) bool {
	if len(currentIDs) != len(requestedIDs) {
		return false
	}
	seen := make(map[string]struct{}, len(currentIDs))
	for _, itemID := range currentIDs {
		seen[itemID] = struct{}{}
	}
	for _, itemID := range requestedIDs {
		if _, ok := seen[itemID]; !ok {
			return false
		}
		delete(seen, itemID)
	}
	return len(seen) == 0
}

package playback

import (
	"context"
	"database/sql"
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
	Items []QueueItem `json:"items"`
}

type Store struct {
	db     *sql.DB
	tracks library.TrackReader
}

func NewStore(db *sql.DB, tracks library.TrackReader) *Store {
	return &Store{db: db, tracks: tracks}
}

func (s *Store) GetQueue(ctx context.Context, userID string) (Queue, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pq.id, pq.track_id, pq.position
		FROM playback_queue pq
		WHERE pq.user_id = ?
		ORDER BY pq.position`, userID)
	if err != nil {
		return Queue{}, fmt.Errorf("get queue: %w", err)
	}
	defer rows.Close()

	items := []QueueItem{}
	for rows.Next() {
		var item QueueItem
		if err := rows.Scan(&item.ID, &item.TrackID, &item.Position); err != nil {
			return Queue{}, err
		}
		track, err := s.tracks.GetTrack(ctx, item.TrackID)
		if err != nil {
			continue
		}
		item.Track = track
		items = append(items, item)
	}
	return Queue{Items: items}, rows.Err()
}

func (s *Store) ReplaceQueue(ctx context.Context, userID string, trackIDs []string) (Queue, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Queue{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM playback_queue WHERE user_id = ?`, userID); err != nil {
		return Queue{}, err
	}

	for i, trackID := range trackIDs {
		if _, err := s.tracks.GetTrack(ctx, trackID); err != nil {
			return Queue{}, library.ErrNotFound
		}
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

func (s *Store) AppendItem(ctx context.Context, userID, trackID string) (Queue, error) {
	if _, err := s.tracks.GetTrack(ctx, trackID); err != nil {
		return Queue{}, library.ErrNotFound
	}

	var maxPos sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MAX(position) FROM playback_queue WHERE user_id = ?`, userID,
	).Scan(&maxPos); err != nil {
		return Queue{}, err
	}

	pos := 0
	if maxPos.Valid {
		pos = int(maxPos.Int64) + 1
	}

	itemID := uuid.NewString()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO playback_queue (id, user_id, position, track_id) VALUES (?, ?, ?, ?)`,
		itemID, userID, pos, trackID,
	); err != nil {
		return Queue{}, err
	}
	return s.GetQueue(ctx, userID)
}

func (s *Store) RemoveItem(ctx context.Context, userID, itemID string) (Queue, error) {
	res, err := s.db.ExecContext(ctx,
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

	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM playback_queue WHERE user_id = ? ORDER BY position`, userID)
	if err != nil {
		return Queue{}, err
	}
	defer rows.Close()

	pos := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return Queue{}, err
		}
		if _, err := s.db.ExecContext(ctx,
			`UPDATE playback_queue SET position = ? WHERE id = ?`, pos, id,
		); err != nil {
			return Queue{}, err
		}
		pos++
	}

	return s.GetQueue(ctx, userID)
}

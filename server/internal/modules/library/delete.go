package library

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	internaldb "github.com/ardam/navidrome-replacement/server/internal/db"
)

type DeleteResult struct {
	DeletedFiles int `json:"deletedFiles"`
}

func (s *Store) DeleteTrack(ctx context.Context, trackID string, removeFile func(path string) error) (DeleteResult, error) {
	return runStoreMutation(ctx, s, "legacy Track deletion", func(store *Store, tx *sql.Tx) (DeleteResult, error) {
		var albumID string
		if err := store.db.QueryRowContext(ctx, `SELECT album_id FROM tracks WHERE id = ?`, trackID).Scan(&albumID); err != nil {
			if err == sql.ErrNoRows {
				return DeleteResult{}, ErrNotFound
			}
			return DeleteResult{}, fmt.Errorf("lookup Track Album: %w", err)
		}
		if err := store.ensureTrackIsNotStaged(ctx, trackID); err != nil {
			return DeleteResult{}, err
		}
		result, err := store.deleteTrack(ctx, trackID, removeFile)
		if err != nil {
			return DeleteResult{}, err
		}
		if err := store.recomputeAlbumGenres(ctx, albumID); err != nil && err != sql.ErrNoRows {
			return DeleteResult{}, fmt.Errorf("recompute deleted Track Album Genres: %w", err)
		}
		if err := internaldb.SynchronizeLegacyAlbum(ctx, tx, albumID); err != nil {
			return DeleteResult{}, err
		}
		if err := internaldb.FinalizeLegacyRemoval(ctx, tx); err != nil {
			return DeleteResult{}, err
		}
		return result, nil
	})
}

func (s *Store) deleteTrack(ctx context.Context, trackID string, removeFile func(path string) error) (DeleteResult, error) {
	var filePath string
	var albumID string
	err := s.db.QueryRowContext(ctx,
		`SELECT file_path, album_id FROM tracks WHERE id = ?`, trackID,
	).Scan(&filePath, &albumID)
	if err == sql.ErrNoRows {
		return DeleteResult{}, ErrNotFound
	}
	if err != nil {
		return DeleteResult{}, fmt.Errorf("lookup track: %w", err)
	}

	result := DeleteResult{}
	if removeFile != nil && filePath != "" {
		if err := removeFile(filePath); err != nil {
			return DeleteResult{}, fmt.Errorf("remove track file: %w", err)
		}
		result.DeletedFiles = 1
	}

	if err := s.deleteTracksByIDs(ctx, []string{trackID}); err != nil {
		return DeleteResult{}, err
	}
	if err := s.cleanupAlbumIfEmpty(ctx, albumID); err != nil {
		return DeleteResult{}, err
	}
	return result, nil
}

func (s *Store) DeleteAlbum(ctx context.Context, albumID string, removeFile func(path string) error) (DeleteResult, error) {
	return runStoreMutation(ctx, s, "legacy Album deletion", func(store *Store, tx *sql.Tx) (DeleteResult, error) {
		result, err := store.deleteAlbum(ctx, albumID, removeFile)
		if err != nil {
			return DeleteResult{}, err
		}
		if err := internaldb.FinalizeLegacyRemoval(ctx, tx); err != nil {
			return DeleteResult{}, err
		}
		return result, nil
	})
}

func (s *Store) deleteAlbum(ctx context.Context, albumID string, removeFile func(path string) error) (DeleteResult, error) {
	var artistID string
	err := s.db.QueryRowContext(ctx,
		`SELECT artist_id FROM albums WHERE id = ?`, albumID,
	).Scan(&artistID)
	if err == sql.ErrNoRows {
		return DeleteResult{}, ErrNotFound
	}
	if err != nil {
		return DeleteResult{}, fmt.Errorf("lookup album: %w", err)
	}
	if stagedErr := s.ensureAlbumIsNotStaged(ctx, albumID); stagedErr != nil {
		return DeleteResult{}, stagedErr
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, file_path FROM tracks WHERE album_id = ?`, albumID,
	)
	if err != nil {
		return DeleteResult{}, fmt.Errorf("list album tracks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	trackIDs := []string{}
	result := DeleteResult{}
	for rows.Next() {
		var trackID, filePath string
		if err := rows.Scan(&trackID, &filePath); err != nil {
			return DeleteResult{}, err
		}
		trackIDs = append(trackIDs, trackID)
		if removeFile != nil && filePath != "" {
			if err := removeFile(filePath); err != nil {
				return DeleteResult{}, fmt.Errorf("remove album track file: %w", err)
			}
			result.DeletedFiles++
		}
	}
	if err := rows.Err(); err != nil {
		return DeleteResult{}, err
	}

	if err := s.deleteTracksByIDs(ctx, trackIDs); err != nil {
		return DeleteResult{}, err
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM albums WHERE id = ?`, albumID); err != nil {
		return DeleteResult{}, fmt.Errorf("delete album: %w", err)
	}
	if err := s.cleanupArtistIfEmpty(ctx, artistID); err != nil {
		return DeleteResult{}, err
	}
	return result, nil
}

func (s *Store) ensureTrackIsNotStaged(ctx context.Context, trackID string) error {
	return s.ensureMigrationIsNotStaged(ctx, `SELECT EXISTS(
		SELECT 1 FROM legacy_migration_copies WHERE source_track_id = ?
	)`, trackID)
}

func (s *Store) ensureAlbumIsNotStaged(ctx context.Context, albumID string) error {
	return s.ensureMigrationIsNotStaged(ctx, `SELECT EXISTS(
		SELECT 1
		FROM legacy_migration_copies
		INNER JOIN tracks ON tracks.id = legacy_migration_copies.source_track_id
		WHERE tracks.album_id = ?
	)`, albumID)
}

func (s *Store) ensureMigrationIsNotStaged(ctx context.Context, query string, identity string) error {
	var isStaged bool
	if err := s.db.QueryRowContext(ctx, query, identity).Scan(&isStaged); err != nil {
		return fmt.Errorf("inspect staged Library Migration copies: %w", err)
	}
	if isStaged {
		return ErrMigrationStaged
	}
	return nil
}

func (s *Store) deleteTracksByIDs(ctx context.Context, trackIDs []string) error {
	if len(trackIDs) == 0 {
		return nil
	}

	placeholders := make([]string, len(trackIDs))
	args := make([]any, len(trackIDs))
	for i, id := range trackIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf(
		`DELETE FROM playback_queue WHERE track_id IN (%s)`,
		strings.Join(placeholders, ", "),
	)
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete queue items: %w", err)
	}

	query = fmt.Sprintf(
		`DELETE FROM playlist_tracks WHERE track_id IN (%s)`,
		strings.Join(placeholders, ", "),
	)
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil && !isMissingTableError(err) {
		return fmt.Errorf("delete playlist tracks: %w", err)
	}
	if err := s.cleanupEmptyPlaylists(ctx); err != nil {
		return err
	}

	query = fmt.Sprintf(`DELETE FROM tracks WHERE id IN (%s)`, strings.Join(placeholders, ", "))
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete tracks: %w", err)
	}
	return nil
}

func isMissingTableError(err error) bool {
	return strings.Contains(err.Error(), "no such table")
}

func (s *Store) cleanupEmptyPlaylists(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM playlists
		WHERE is_default = 0
			AND NOT EXISTS (
				SELECT 1
				FROM playlist_tracks
				WHERE playlist_tracks.playlist_id = playlists.id
			)`)
	if err != nil && !isMissingTableError(err) {
		return fmt.Errorf("delete empty playlists: %w", err)
	}
	return nil
}

func (s *Store) cleanupAlbumIfEmpty(ctx context.Context, albumID string) error {
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tracks WHERE album_id = ?`, albumID,
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	var artistID string
	if err := s.db.QueryRowContext(ctx,
		`SELECT artist_id FROM albums WHERE id = ?`, albumID,
	).Scan(&artistID); err == sql.ErrNoRows {
		return nil
	} else if err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM albums WHERE id = ?`, albumID); err != nil {
		return err
	}
	return s.cleanupArtistIfEmpty(ctx, artistID)
}

func (s *Store) cleanupArtistIfEmpty(ctx context.Context, artistID string) error {
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM albums WHERE artist_id = ?`, artistID,
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM artists WHERE id = ?`, artistID)
	return err
}

func removeMusicFile(path string, roots []string) error {
	if path == "" {
		return nil
	}
	if !isPathUnderRoots(path, roots) {
		return nil
	}
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

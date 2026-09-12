package searchdata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Rebuild atomically replaces derived data. Original records and audio are never
// written, and readers retain the previous snapshot until the commit succeeds.
func (store *Store) Rebuild(ctx context.Context) error {
	return store.rebuild(ctx, false)
}

// EnsureCurrent backfills missing records and upgrades normalization rules at
// startup, before the database is handed to request handlers.
func (store *Store) EnsureCurrent(ctx context.Context) error {
	return store.rebuild(ctx, true)
}

func (store *Store) rebuild(ctx context.Context, onlyIfNeeded bool) (returnErr error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Library Search rebuild: %w", err)
	}
	defer func() {
		if err := transaction.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback Library Search rebuild: %w", err))
		}
	}()
	if onlyIfNeeded {
		isCurrent, err := hasCurrentData(ctx, transaction)
		if err != nil {
			return err
		}
		if isCurrent {
			return nil
		}
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM library_search_texts;
 INSERT INTO library_search_texts
 SELECT kind, id, library_search_text(name, kind), library_search_version() FROM library_search_sources`); err != nil {
		return fmt.Errorf("rebuild Library Search data: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit Library Search rebuild: %w", err)
	}
	return nil
}

func hasCurrentData(ctx context.Context, transaction *sql.Tx) (bool, error) {
	var isCurrent bool
	err := transaction.QueryRowContext(ctx, `SELECT NOT EXISTS (
 SELECT 1 FROM library_search_sources source
 LEFT JOIN library_search_texts prepared ON prepared.kind = source.kind AND prepared.id = source.id
 WHERE prepared.id IS NULL OR prepared.rules_version != ?
 )`, RULES_VERSION).Scan(&isCurrent)
	if err != nil {
		return false, fmt.Errorf("check Library Search rules version: %w", err)
	}
	return isCurrent, nil
}

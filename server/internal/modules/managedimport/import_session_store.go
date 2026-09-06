package managedimport

import (
	"context"
	"fmt"
)

func (store *Store) HeartbeatBatch(ctx context.Context, batchID string) error {
	result, err := store.database.ExecContext(ctx, `
		UPDATE managed_import_batches SET updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status <> ?`, batchID, BATCH_STATUS_COMPLETED)
	if err != nil {
		return fmt.Errorf("refresh Managed Import Batch %q liveness: %w", batchID, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect Managed Import Batch %q liveness: %w", batchID, err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

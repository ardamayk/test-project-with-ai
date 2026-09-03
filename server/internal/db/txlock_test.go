package db_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/db"
)

// Two pooled connections that each read before writing must both commit. With
// deferred transactions the second writer fails with SQLITE_BUSY instead of
// waiting, and its failed COMMIT leaves the connection inside an open
// transaction that poisons every later request (observed as "cannot start a
// transaction within a transaction" during the Managed Import e2e journey).
func TestConcurrentReadThenWriteTransactionsWaitInsteadOfFailing(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "txlock.db")
	sqlDB, err := db.OpenAndMigrate(ctx, databasePath, migrationsDir(t))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	sqlDB.SetMaxOpenConns(2)

	if _, err := sqlDB.ExecContext(ctx, `CREATE TABLE txlock_probe (id INTEGER PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("create probe table: %v", err)
	}

	readThenWrite := func(value string) error {
		tx, err := sqlDB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM txlock_probe`).Scan(&count); err != nil {
			return err
		}
		time.Sleep(150 * time.Millisecond)
		if _, err := tx.ExecContext(ctx, `INSERT INTO txlock_probe (value) VALUES (?)`, value); err != nil {
			return err
		}
		return tx.Commit()
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for index := range errs {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			errs[index] = readThenWrite("writer")
		}(index)
	}
	wg.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("writer %d failed: %v", index, err)
		}
	}

	// Every pooled connection must still be able to begin a new transaction.
	for attempt := 0; attempt < 4; attempt++ {
		tx, err := sqlDB.BeginTx(ctx, &sql.TxOptions{})
		if err != nil {
			t.Fatalf("begin follow-up transaction %d: %v", attempt, err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("roll back follow-up transaction %d: %v", attempt, err)
		}
	}
	var count int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM txlock_probe`).Scan(&count); err != nil {
		t.Fatalf("count probe rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("probe rows = %d, want 2", count)
	}
}

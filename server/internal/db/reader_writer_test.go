package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/db"
)

func TestWriterCommitsWhileReaderIsActive(t *testing.T) {
	ctx := context.Background()
	sqlDB, err := db.OpenAndMigrate(ctx, filepath.Join(t.TempDir(), "reader-writer.db"), migrationsDir(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()
	if _, err := sqlDB.ExecContext(ctx, `CREATE TABLE concurrency_probe (id INTEGER PRIMARY KEY); INSERT INTO concurrency_probe VALUES (1), (2)`); err != nil {
		t.Fatal(err)
	}
	reader, err := sqlDB.QueryContext(ctx, `SELECT id FROM concurrency_probe`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	if !reader.Next() {
		t.Fatalf("start active reader: %v", reader.Err())
	}
	writer, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Rollback() }()
	if _, err := writer.ExecContext(ctx, `INSERT INTO concurrency_probe VALUES (3)`); err != nil {
		t.Fatal(err)
	}
	if err := writer.Commit(); err != nil {
		t.Fatalf("commit with an active reader: %v", err)
	}
	var count int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM concurrency_probe`).Scan(&count); err != nil {
		t.Fatalf("read after concurrent commit: %v", err)
	}
	if count != 3 {
		t.Fatalf("committed rows = %d, want 3", count)
	}
}

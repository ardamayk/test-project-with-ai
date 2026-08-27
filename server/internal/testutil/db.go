package testutil

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/db"
)

func OpenMigratedDB(t *testing.T) *sql.DB {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve testutil path")
	}
	migrationsDir := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations"))
	databasePath := filepath.Join(t.TempDir(), "test.db")
	sqlDB, err := db.OpenAndMigrate(context.Background(), databasePath, migrationsDir)
	if err != nil {
		t.Fatalf("open migrated db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}

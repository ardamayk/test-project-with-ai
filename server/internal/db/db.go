package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

func OpenAndMigrate(ctx context.Context, databasePath string, migrationsDir string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	// Transactions start with BEGIN IMMEDIATE so a writer takes the write lock
	// up front and waits through busy_timeout. A deferred transaction that reads
	// first and writes later fails with SQLITE_BUSY immediately when another
	// connection holds the write lock; that failed COMMIT also leaves the pooled
	// connection inside an open transaction, after which every later request on
	// it fails with "cannot start a transaction within a transaction".
	dsn := fmt.Sprintf("file:%s?_txlock=immediate&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", databasePath)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	if err := goose.SetDialect("sqlite"); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("set migration dialect: %w", err)
	}
	if err := goose.Up(sqlDB, migrationsDir); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	if err := BackfillExpandedLibrary(ctx, sqlDB); err != nil {
		backfillErr := fmt.Errorf("backfill expanded library: %w", err)
		if closeErr := sqlDB.Close(); closeErr != nil {
			return nil, errors.Join(backfillErr, fmt.Errorf("close database after backfill failure: %w", closeErr))
		}
		return nil, backfillErr
	}

	return sqlDB, nil
}

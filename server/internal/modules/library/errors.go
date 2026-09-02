package library

import "errors"

var (
	ErrNotFound        = errors.New("not found")
	ErrScanRunning     = errors.New("scan already running")
	ErrNoMusicPaths    = errors.New("no music paths configured")
	ErrMigrationStaged = errors.New("legacy Track has a staged Library Migration copy")
)

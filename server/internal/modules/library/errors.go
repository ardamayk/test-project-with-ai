package library

import "errors"

var (
	ErrNotFound        = errors.New("not found")
	ErrMigrationStaged = errors.New("legacy Track has a staged Library Migration copy")
	ErrManagedAlbum    = errors.New("managed albums require per-track permanent deletion")
)

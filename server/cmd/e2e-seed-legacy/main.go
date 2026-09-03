// Command e2e-seed-legacy registers an existing audio file as an Indexed
// Legacy Track so browser end-to-end tests can exercise Library Migration and
// Legacy Source Cleanup. The Music Server never scans MUSIC_PATHS (ADR 0015),
// so this fixture-only path is the only way a test can create legacy state.
//
// It is intended for web/e2e only and must never run against a production
// database.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ardam/navidrome-replacement/server/internal/db"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e-seed-legacy:", err)
		os.Exit(1)
	}
}

func run() error {
	databasePath := flag.String("database", "", "SQLite database path used by the Music Server under test")
	filePath := flag.String("file", "", "absolute path of the legacy audio file (must live under MUSIC_PATHS)")
	title := flag.String("title", "", "Track title")
	artist := flag.String("artist", "", "Track Artist")
	albumArtist := flag.String("album-artist", "", "Album Artist")
	album := flag.String("album", "", "Album title")
	genre := flag.String("genre", "", "Genre")
	trackNumber := flag.Int("track", 1, "track number")
	year := flag.Int("year", 2026, "release year")
	flag.Parse()

	for name, value := range map[string]string{
		"database": *databasePath, "file": *filePath, "title": *title,
		"artist": *artist, "album-artist": *albumArtist, "album": *album, "genre": *genre,
	} {
		if value == "" {
			return fmt.Errorf("-%s is required", name)
		}
	}
	absolutePath, err := filepath.Abs(*filePath)
	if err != nil {
		return fmt.Errorf("resolve legacy file: %w", err)
	}
	info, err := os.Stat(absolutePath)
	if err != nil {
		return fmt.Errorf("stat legacy file: %w", err)
	}

	ctx := context.Background()
	migrationsDir := "migrations"
	if _, statErr := os.Stat(migrationsDir); os.IsNotExist(statErr) {
		migrationsDir = filepath.Join("server", "migrations")
	}
	sqlDB, err := db.OpenAndMigrate(ctx, *databasePath, migrationsDir)
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()

	store := library.NewStore(sqlDB)
	metadata := library.FileMetadata{
		Path:         absolutePath,
		Format:       "mp3",
		SizeBytes:    info.Size(),
		ModTime:      info.ModTime(),
		Title:        *title,
		Artist:       *artist,
		AlbumArtist:  *albumArtist,
		Album:        *album,
		TrackNo:      *trackNumber,
		Year:         *year,
		DurationMs:   1000,
		Genre:        *genre,
		SampleRateHz: 22050,
	}
	if _, _, err := store.SeedLegacyTrack(ctx, metadata); err != nil {
		return fmt.Errorf("seed legacy Track: %w", err)
	}

	var trackID string
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT track_id FROM track_sources WHERE file_path = ? AND source_kind = 'legacy'`,
		absolutePath).Scan(&trackID); err != nil {
		return fmt.Errorf("read seeded Track ID: %w", err)
	}
	fmt.Println(trackID)
	return nil
}

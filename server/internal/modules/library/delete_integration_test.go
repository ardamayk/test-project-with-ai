package library_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

func setupLibraryDB(t *testing.T) *sql.DB {
	t.Helper()
	db := testutil.OpenMigratedDB(t)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	return db
}

func TestAlbumArtistGroupsFeaturedTrackUnderAlbumArtist(t *testing.T) {
	db := setupLibraryDB(t)

	store := library.NewStore(db)
	meta := library.FileMetadata{
		Path:        filepath.Join(t.TempDir(), "snow.flac"),
		Format:      "flac",
		SizeBytes:   10,
		ModTime:     time.Now(),
		Title:       "Snow On The Beach (feat. Lana Del Rey)",
		Artist:      "Lana Del Rey",
		AlbumArtist: "Taylor Swift",
		Album:       "Midnights",
		TrackNo:     4,
		Year:        2022,
		DurationMs:  256000,
		Genre:       "Synthpop",
	}
	if err := os.WriteFile(meta.Path, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.UpsertFromScan(context.Background(), meta); err != nil {
		t.Fatal(err)
	}

	var artistName, trackArtist string
	err := db.QueryRow(`
		SELECT ar.name, t.artist_name
		FROM albums al
		INNER JOIN artists ar ON ar.id = al.artist_id
		INNER JOIN tracks t ON t.album_id = al.id`,
	).Scan(&artistName, &trackArtist)
	if err != nil {
		t.Fatal(err)
	}
	if artistName != "Taylor Swift" {
		t.Fatalf("album artist = %q, want Taylor Swift", artistName)
	}
	if trackArtist != "Lana Del Rey" {
		t.Fatalf("track artist = %q, want Lana Del Rey", trackArtist)
	}

	var albumCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM albums`).Scan(&albumCount); err != nil {
		t.Fatal(err)
	}
	if albumCount != 1 {
		t.Fatalf("album count = %d, want 1", albumCount)
	}
}

func TestDeleteTrackRemovesDatabaseRowsAndFile(t *testing.T) {
	tempDir := t.TempDir()
	trackPath := filepath.Join(tempDir, "track.flac")
	if err := os.WriteFile(trackPath, []byte("fake-audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := setupLibraryDB(t)

	store := library.NewStore(db)
	meta := library.FileMetadata{
		Path:        trackPath,
		Format:      "flac",
		SizeBytes:   10,
		ModTime:     time.Now(),
		Title:       "Track",
		Artist:      "Taylor Swift",
		AlbumArtist: "Taylor Swift",
		Album:       "Midnights",
		TrackNo:     1,
		Year:        2022,
		DurationMs:  1000,
		Genre:       "Pop",
	}
	if _, _, err := store.UpsertFromScan(context.Background(), meta); err != nil {
		t.Fatal(err)
	}

	var trackID string
	if err := db.QueryRow(`SELECT id FROM tracks`).Scan(&trackID); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DeleteTrack(context.Background(), trackID, os.Remove); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(trackPath); !os.IsNotExist(err) {
		t.Fatalf("track file still exists: %v", err)
	}

	var trackCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tracks`).Scan(&trackCount); err != nil {
		t.Fatal(err)
	}
	if trackCount != 0 {
		t.Fatalf("track count = %d, want 0", trackCount)
	}
}

func TestDeleteBlocksTracksAndAlbumsWithPendingMigrationCopies(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		delete func(*library.Store, string, string) error
	}{
		{
			name: "Track",
			delete: func(store *library.Store, trackID, _ string) error {
				_, err := store.DeleteTrack(context.Background(), trackID, os.Remove)
				return err
			},
		},
		{
			name: "Album",
			delete: func(store *library.Store, _, albumID string) error {
				_, err := store.DeleteAlbum(context.Background(), albumID, os.Remove)
				return err
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			database := setupLibraryDB(t)
			store := library.NewStore(database)
			trackPath := filepath.Join(t.TempDir(), "legacy.flac")
			if err := os.WriteFile(trackPath, []byte("legacy audio"), 0o640); err != nil {
				t.Fatalf("write Legacy Track: %v", err)
			}
			metadata := library.FileMetadata{
				Path: trackPath, Format: "flac", SizeBytes: 12, ModTime: time.Now(), Title: "Legacy Track",
				Artist: "Legacy Artist", AlbumArtist: "Legacy Artist", Album: "Legacy Album", TrackNo: 1,
				DurationMs: 1000, SampleRateHz: 44100, BitDepth: 16,
			}
			if _, _, err := store.UpsertFromScan(context.Background(), metadata); err != nil {
				t.Fatalf("seed Legacy Track: %v", err)
			}
			var trackID, albumID string
			if err := database.QueryRow(`SELECT id, album_id FROM tracks WHERE file_path = ?`, trackPath).Scan(&trackID, &albumID); err != nil {
				t.Fatalf("read Legacy Track identity: %v", err)
			}
			validHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			if _, err := database.Exec(`
				INSERT INTO legacy_migration_copies (
					source_track_id, pending_track_id, pending_album_id, pending_album_artist_id,
					source_file_path, pending_audio_path, pending_artwork_path, source_sha256,
					pending_sha256, artwork_sha256, inspection_json, status
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '{}', 'verified')`,
				trackID, "pending-track-"+trackID, "pending-album-"+trackID, "pending-artist-"+trackID,
				trackPath, filepath.Join(t.TempDir(), "pending.flac"), filepath.Join(t.TempDir(), "cover.png"),
				validHash, validHash, validHash,
			); err != nil {
				t.Fatalf("seed verified migration copy: %v", err)
			}

			err := testCase.delete(store, trackID, albumID)
			if !errors.Is(err, library.ErrMigrationStaged) {
				t.Fatalf("delete staged %s error = %v", testCase.name, err)
			}
			if _, err := os.Stat(trackPath); err != nil {
				t.Fatalf("Legacy source changed after blocked deletion: %v", err)
			}
			var trackCount, copyCount int
			if err := database.QueryRow(`SELECT COUNT(*) FROM tracks WHERE id = ?`, trackID).Scan(&trackCount); err != nil {
				t.Fatalf("count retained Legacy Track: %v", err)
			}
			if err := database.QueryRow(`SELECT COUNT(*) FROM legacy_migration_copies WHERE source_track_id = ?`, trackID).Scan(&copyCount); err != nil {
				t.Fatalf("count retained migration copy: %v", err)
			}
			if trackCount != 1 || copyCount != 1 {
				t.Fatalf("retained rows = (%d Tracks, %d migration copies)", trackCount, copyCount)
			}
		})
	}
}

func TestDeleteAlbumRemovesTracksAndFiles(t *testing.T) {
	tempDir := t.TempDir()
	trackPath := filepath.Join(tempDir, "track.flac")
	if err := os.WriteFile(trackPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := setupLibraryDB(t)

	store := library.NewStore(db)
	meta := library.FileMetadata{
		Path:         trackPath,
		Format:       "flac",
		SizeBytes:    10,
		ModTime:      time.Now(),
		Title:        "Track",
		Artist:       "Taylor Swift",
		AlbumArtist:  "Taylor Swift",
		Album:        "Midnights",
		TrackNo:      1,
		Year:         2022,
		DurationMs:   1000,
		SampleRateHz: 44100,
		BitDepth:     16,
		Genre:        "Pop",
	}
	if _, _, err := store.UpsertFromScan(context.Background(), meta); err != nil {
		t.Fatal(err)
	}

	var albumID string
	if err := db.QueryRow(`SELECT album_id FROM tracks`).Scan(&albumID); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DeleteAlbum(context.Background(), albumID, os.Remove); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(trackPath); !os.IsNotExist(err) {
		t.Fatalf("album track file still exists: %v", err)
	}

	var albumCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM albums`).Scan(&albumCount); err != nil {
		t.Fatal(err)
	}
	if albumCount != 0 {
		t.Fatalf("album count = %d, want 0", albumCount)
	}
}

func TestDeleteTrackRemovesQueueAndEmptyAlbum(t *testing.T) {
	tempDir := t.TempDir()
	trackPath := filepath.Join(tempDir, "track.flac")
	if err := os.WriteFile(trackPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := setupLibraryDB(t)

	store := library.NewStore(db)
	meta := library.FileMetadata{
		Path:        trackPath,
		Format:      "flac",
		SizeBytes:   10,
		ModTime:     time.Now(),
		Title:       "Only Track",
		Artist:      "Solo Artist",
		AlbumArtist: "Solo Artist",
		Album:       "Solo Album",
		TrackNo:     1,
		Year:        2024,
		DurationMs:  1000,
		Genre:       "Indie",
	}
	if _, _, err := store.UpsertFromScan(context.Background(), meta); err != nil {
		t.Fatal(err)
	}

	var trackID string
	if err := db.QueryRow(`SELECT id FROM tracks`).Scan(&trackID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO playback_queue (id, user_id, position, track_id) VALUES (?, ?, ?, ?)`,
		"q1", "user", 0, trackID,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DeleteTrack(context.Background(), trackID, os.Remove); err != nil {
		t.Fatal(err)
	}

	var queueCount, albumCount, artistCount int
	for _, query := range []struct {
		sql  string
		dest *int
	}{
		{`SELECT COUNT(*) FROM playback_queue`, &queueCount},
		{`SELECT COUNT(*) FROM albums`, &albumCount},
		{`SELECT COUNT(*) FROM artists`, &artistCount},
	} {
		if err := db.QueryRow(query.sql).Scan(query.dest); err != nil {
			t.Fatal(err)
		}
	}
	if queueCount != 0 || albumCount != 0 || artistCount != 0 {
		t.Fatalf("queue=%d album=%d artist=%d, want all 0", queueCount, albumCount, artistCount)
	}
}

func TestDeleteTrackRemovesEmptyUserPlaylist(t *testing.T) {
	tempDir := t.TempDir()
	trackPath := filepath.Join(tempDir, "track.flac")
	if err := os.WriteFile(trackPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := setupLibraryDB(t)

	store := library.NewStore(db)
	meta := library.FileMetadata{
		Path:        trackPath,
		Format:      "flac",
		SizeBytes:   10,
		ModTime:     time.Now(),
		Title:       "Only Track",
		Artist:      "Solo Artist",
		AlbumArtist: "Solo Artist",
		Album:       "Solo Album",
		TrackNo:     1,
		DurationMs:  1000,
		Genre:       "Indie",
	}
	if _, _, err := store.UpsertFromScan(context.Background(), meta); err != nil {
		t.Fatal(err)
	}

	var trackID string
	if err := db.QueryRow(`SELECT id FROM tracks`).Scan(&trackID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO playlists (id, user_id, name, is_default) VALUES (?, ?, ?, 0)`,
		"playlist-1", "user", "Temporary",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO playlist_tracks (playlist_id, track_id, position) VALUES (?, ?, ?)`,
		"playlist-1", trackID, 0,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DeleteTrack(context.Background(), trackID, os.Remove); err != nil {
		t.Fatal(err)
	}

	var playlistCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM playlists WHERE id = ?`, "playlist-1").Scan(&playlistCount); err != nil {
		t.Fatal(err)
	}
	if playlistCount != 0 {
		t.Fatalf("playlist count = %d, want 0", playlistCount)
	}
}

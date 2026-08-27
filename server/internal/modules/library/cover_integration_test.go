package library_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
)

func TestScanBackfillsAlbumCoverFromFlac(t *testing.T) {
	flacPath := filepath.Join("..", "..", "..", "music", "Taylor Swift _The Life of a Showgirl _01_The Fate of Ophelia.flac")
	if _, err := os.Stat(flacPath); err != nil {
		t.Skip("sample flac not present")
	}

	db := setupLibraryDB(t)

	absPath, _ := filepath.Abs(flacPath)
	store := library.NewStore(db)
	files, err := library.WalkMusicPaths([]string{filepath.Dir(absPath)})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("expected scanned files")
	}
	if len(files[0].Metadata.CoverData) == 0 {
		t.Fatal("expected embedded cover in flac metadata")
	}

	_, _, err = store.UpsertFromScan(context.Background(), files[0].Metadata)
	if err != nil {
		t.Fatal(err)
	}

	var coverLen int
	err = db.QueryRow(`SELECT length(cover_data) FROM albums`).Scan(&coverLen)
	if err != nil {
		t.Fatal(err)
	}
	if coverLen == 0 {
		t.Fatalf("expected cover stored in albums table")
	}
}

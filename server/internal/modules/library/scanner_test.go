package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsSupportedFile(t *testing.T) {
	tests := []struct {
		name   string
		format string
		ok     bool
	}{
		{"track.mp3", "mp3", true},
		{"track.FLAC", "flac", true},
		{"readme.txt", "", false},
		{"song.ogg", "ogg", true},
	}

	for _, tc := range tests {
		format, ok := isSupportedFile(tc.name)
		if ok != tc.ok || (tc.ok && format != tc.format) {
			t.Fatalf("%s: got (%s, %v), want (%s, %v)", tc.name, format, ok, tc.format, tc.ok)
		}
	}
}

func TestWalkMusicPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Artist - Album - Song.mp3")
	if err := os.WriteFile(path, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := WalkMusicPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Metadata.Title == "" {
		t.Fatal("expected fallback title")
	}
}

func TestSortKey(t *testing.T) {
	if sortKey("  Hello ") != "hello" {
		t.Fatal("sortKey normalization failed")
	}
}

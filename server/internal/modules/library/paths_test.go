package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsPathUnderRoots(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "music", "album")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	track := filepath.Join(nested, "song.flac")
	if err := os.WriteFile(track, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !isPathUnderRoots(track, []string{root}) {
		t.Fatal("expected track under root")
	}
	if isPathUnderRoots(track, []string{filepath.Join(root, "other")}) {
		t.Fatal("expected track outside unrelated root")
	}
	if isPathUnderRoots("", []string{root}) {
		t.Fatal("empty path should not match")
	}
}

func TestRemoveMusicFileOnlyUnderConfiguredRoots(t *testing.T) {
	root := t.TempDir()
	under := filepath.Join(root, "album", "track.flac")
	if err := os.MkdirAll(filepath.Dir(under), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(under, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	outsideRoot := t.TempDir()
	outside := filepath.Join(outsideRoot, "track.flac")
	if err := os.WriteFile(outside, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := removeMusicFile(under, []string{root}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(under); !os.IsNotExist(err) {
		t.Fatal("expected file under root to be removed")
	}

	if err := removeMusicFile(outside, []string{root}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("expected file outside root to remain")
	}
}

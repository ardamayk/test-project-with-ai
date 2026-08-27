package playback

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestServeTrackFileRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.mp3")
	content := []byte("0123456789abcdef")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	req.Header.Set("Range", "bytes=0-4")
	rec := httptest.NewRecorder()

	if err := ServeTrackFile(rec, req, path); err != nil {
		t.Fatal(err)
	}

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 0-4/16" {
		t.Fatalf("unexpected Content-Range: %q", got)
	}
	if rec.Body.String() != "01234" {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}
}

func TestContentTypeForPath(t *testing.T) {
	if ct := contentTypeForPath("/music/song.flac"); ct != "audio/flac" {
		t.Fatalf("expected audio/flac, got %s", ct)
	}
	if ct := contentTypeForPath("/music/song.opus"); ct != "audio/ogg" {
		t.Fatalf("expected audio/ogg, got %s", ct)
	}
}

package radio

import (
	"io"
	"strings"
	"testing"
)

func TestValidateStreamURLBlocksLocalTargets(t *testing.T) {
	for _, rawURL := range []string{
		"file:///tmp/song.mp3",
		"http://localhost:8000/stream",
		"http://127.0.0.1:8000/stream",
		"http://10.0.0.1/stream",
		"http://169.254.1.1/stream",
	} {
		if err := ValidateStreamURL(rawURL); err == nil {
			t.Fatalf("ValidateStreamURL(%q) = nil, want error", rawURL)
		}
	}
	if err := ValidateStreamURL("https://stream.radioparadise.com/mp3-192"); err != nil {
		t.Fatalf("public URL rejected: %v", err)
	}
}

func TestICYReaderStripsMetadataAndReportsNowPlaying(t *testing.T) {
	var updates []NowPlaying
	reader := NewICYReader(
		io.NopCloser(strings.NewReader("abcd\x02StreamTitle='Artist - Title';\x00\x00\x00")),
		4,
		func(now NowPlaying) {
			updates = append(updates, now)
		},
	)

	audio, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(audio) != "abcd" {
		t.Fatalf("audio = %q", string(audio))
	}
	if len(updates) != 1 || updates[0].Artist != "Artist" || updates[0].Title != "Title" {
		t.Fatalf("updates = %+v", updates)
	}
}

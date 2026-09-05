package identification_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/identification"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMusicBrainzRecordingLookupRequestsIncludesAndParsesRecording(t *testing.T) {
	var received *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, "musicbrainz-recording.json"))
	}))
	defer server.Close()
	client := identification.NewMusicBrainzClient(server.Client(), server.URL, "EarthlyAudio/test")

	recording, err := client.Recording(context.Background(), "5bcd7ba9-3b1f-4f1a-8a5a-8b0c9d1e2f30")
	if err != nil {
		t.Fatalf("Recording() error = %v", err)
	}

	if received.URL.Path != "/ws/2/recording/5bcd7ba9-3b1f-4f1a-8a5a-8b0c9d1e2f30" {
		t.Fatalf("path = %s", received.URL.Path)
	}
	if received.URL.RawQuery != "fmt=json&inc=artist-credits+isrcs+releases+release-groups+genres" {
		t.Fatalf("query = %s", received.URL.RawQuery)
	}
	if received.Header.Get("User-Agent") != "EarthlyAudio/test" || received.Header.Get("Accept") != "application/json" {
		t.Fatalf("headers = %v", received.Header)
	}
	if recording.ID != "5bcd7ba9-3b1f-4f1a-8a5a-8b0c9d1e2f30" || recording.Title != "Welcome to New York (Taylor's Version)" {
		t.Fatalf("recording = %+v", recording)
	}
	if len(recording.Artists) != 1 || recording.Artists[0].Name != "Taylor Swift" || recording.Artists[0].ID != "20244d07-534f-4eff-b4d4-930878889970" {
		t.Fatalf("artists = %+v", recording.Artists)
	}
	if len(recording.ISRCs) != 1 || recording.ISRCs[0] != "USUG12306672" {
		t.Fatalf("isrcs = %v", recording.ISRCs)
	}
	if len(recording.Genres) != 2 || recording.Genres[0] != "pop" {
		t.Fatalf("genres = %v", recording.Genres)
	}
	if len(recording.Releases) != 2 {
		t.Fatalf("releases = %+v", recording.Releases)
	}
	deluxe := recording.Releases[1]
	if deluxe.ID != "22222222-2222-2222-2222-222222222222" || deluxe.Title != "1989 (Taylor's Version) [Deluxe]" || deluxe.Status != "Official" {
		t.Fatalf("deluxe = %+v", deluxe)
	}
	if deluxe.Date != "2023-10-27" || deluxe.Year != 2023 {
		t.Fatalf("deluxe date = %q year %d", deluxe.Date, deluxe.Year)
	}
	if deluxe.ReleaseGroup.ID != "aaaa1111-1111-1111-1111-111111111111" || deluxe.ReleaseGroup.PrimaryType != "Album" {
		t.Fatalf("deluxe group = %+v", deluxe.ReleaseGroup)
	}
	if len(deluxe.AlbumArtists) != 1 || deluxe.AlbumArtists[0].Name != "Taylor Swift" {
		t.Fatalf("deluxe album artists = %+v", deluxe.AlbumArtists)
	}
}

func TestMusicBrainzRecordingLookupMapsNotFoundAndFailures(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   error
	}{
		"not found":    {http.StatusNotFound, `{"error":"Not Found"}`, identification.ErrRecordingNotFound},
		"rate limited": {http.StatusServiceUnavailable, `rate limit`, identification.ErrServiceUnavailable},
		"malformed":    {http.StatusOK, `{"id":`, identification.ErrServiceUnavailable},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(testCase.status)
				_, _ = w.Write([]byte(testCase.body))
			}))
			defer server.Close()
			client := identification.NewMusicBrainzClient(server.Client(), server.URL, "EarthlyAudio/test")

			_, err := client.Recording(context.Background(), "mbid")

			if !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestMusicBrainzRecordingIDsByISRCParsesRecordingsAndMapsNotFound(t *testing.T) {
	var received *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r
		if r.URL.Path == "/ws/2/isrc/UNKNOWN00001" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"Not Found"}`))
			return
		}
		_, _ = w.Write([]byte(`{"isrc":"USUG12306672","recordings":[{"id":"rec-a","title":"A"},{"id":"rec-b","title":"B"}]}`))
	}))
	defer server.Close()
	client := identification.NewMusicBrainzClient(server.Client(), server.URL, "EarthlyAudio/test")

	ids, err := client.RecordingIDsByISRC(context.Background(), "USUG12306672")
	if err != nil {
		t.Fatalf("RecordingIDsByISRC() error = %v", err)
	}
	if received.URL.Path != "/ws/2/isrc/USUG12306672" || received.URL.RawQuery != "fmt=json" {
		t.Fatalf("request = %s?%s", received.URL.Path, received.URL.RawQuery)
	}
	if len(ids) != 2 || ids[0] != "rec-a" {
		t.Fatalf("ids = %v", ids)
	}

	_, err = client.RecordingIDsByISRC(context.Background(), "UNKNOWN00001")
	if !errors.Is(err, identification.ErrRecordingNotFound) {
		t.Fatalf("error = %v, want ErrRecordingNotFound", err)
	}
}

package identification_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/identification"
)

// TestNewIdentifierRunsFpcalcAgainstFakeServices proves the production wiring:
// the real fpcalc program, the form-encoded AcoustID lookup and the
// MusicBrainz recording fetch, all pointed at local fake servers through the
// configured base URLs, with the project User-Agent on every request.
func TestNewIdentifierRunsFpcalcAgainstFakeServices(t *testing.T) {
	if _, err := exec.LookPath("fpcalc"); err != nil {
		t.Skip("fpcalc not installed on this host")
	}
	var userAgents []string
	acoustID := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgents = append(userAgents, r.Header.Get("User-Agent"))
		if err := r.ParseForm(); err != nil || r.PostForm.Get("client") != "test-key" || r.PostForm.Get("fingerprint") == "" {
			t.Errorf("unexpected AcoustID form: %v", r.PostForm)
		}
		_, _ = w.Write([]byte(`{"status":"ok","results":[{"id":"a-1","score":0.98,"recordings":[{"id":"5bcd7ba9-3b1f-4f1a-8a5a-8b0c9d1e2f30","sources":12}]}]}`))
	}))
	defer acoustID.Close()
	musicBrainz := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgents = append(userAgents, r.Header.Get("User-Agent"))
		_, _ = w.Write(fixture(t, "musicbrainz-recording.json"))
	}))
	defer musicBrainz.Close()
	cfg := config.RecordingIdentificationConfig{
		Enabled:            true,
		MinScore:           0.90,
		AcoustIDAPIKey:     "test-key",
		AcoustIDBaseURL:    acoustID.URL,
		MusicBrainzBaseURL: musicBrainz.URL,
	}

	result := identification.NewIdentifier(cfg, "0.1.0-test", "fpcalc", nil).Identify(context.Background(), synthesizedTone(t), identification.Hint{})

	if result.Outcome != identification.OUTCOME_MATCHED {
		t.Fatalf("outcome = %s (%s)", result.Outcome, result.Reason)
	}
	if result.Recording.Title != "Welcome to New York (Taylor's Version)" || result.Score != 0.98 {
		t.Fatalf("result = %+v", result)
	}
	if len(userAgents) != 2 {
		t.Fatalf("requests = %v", userAgents)
	}
	for _, userAgent := range userAgents {
		if !strings.HasPrefix(userAgent, "EarthlyAudio/0.1.0-test (https://") {
			t.Fatalf("User-Agent = %q", userAgent)
		}
	}
}

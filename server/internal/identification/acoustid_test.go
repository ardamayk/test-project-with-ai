package identification_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/identification"
)

const acoustIDResponse = `{"status":"ok","results":[
	{"id":"acoustid-1","score":0.97,"recordings":[{"id":"rec-a","sources":40},{"id":"rec-b","sources":2}]},
	{"id":"acoustid-2","score":0.41,"recordings":[{"id":"rec-c","sources":7}]}
]}`

func TestAcoustIDLookupPostsFingerprintAndParsesRecordings(t *testing.T) {
	var received *http.Request
	var form map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(acoustIDResponse))
	}))
	defer server.Close()
	client := identification.NewAcoustIDClient(server.Client(), server.URL, "app-key", "EarthlyAudio/test")

	results, err := client.Lookup(context.Background(), identification.Fingerprint{DurationSeconds: 212.64, Value: "AQADtPkS"})
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}

	if received.Method != http.MethodPost || received.URL.Path != "/v2/lookup" {
		t.Fatalf("request = %s %s", received.Method, received.URL.Path)
	}
	if received.Header.Get("User-Agent") != "EarthlyAudio/test" {
		t.Fatalf("User-Agent = %q", received.Header.Get("User-Agent"))
	}
	want := map[string]string{"client": "app-key", "duration": "212", "fingerprint": "AQADtPkS", "meta": "recordings sources"}
	for key, value := range want {
		if len(form[key]) != 1 || form[key][0] != value {
			t.Fatalf("form[%s] = %v, want %q", key, form[key], value)
		}
	}
	if len(results) != 2 || results[0].ID != "acoustid-1" || results[0].Score != 0.97 {
		t.Fatalf("results = %+v", results)
	}
	if len(results[0].Recordings) != 2 || results[0].Recordings[0].ID != "rec-a" || results[0].Recordings[0].Sources != 40 {
		t.Fatalf("recordings = %+v", results[0].Recordings)
	}
}

func TestAcoustIDLookupReportsServiceErrors(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"application error":   {http.StatusOK, `{"status":"error","error":{"code":4,"message":"invalid API key"}}`},
		"http failure":        {http.StatusServiceUnavailable, `busy`},
		"malformed body":      {http.StatusOK, `{"status":"ok","results":`},
		"rate limit exceeded": {http.StatusTooManyRequests, `{"status":"error","error":{"code":14,"message":"too many requests"}}`},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(testCase.status)
				_, _ = w.Write([]byte(testCase.body))
			}))
			defer server.Close()
			client := identification.NewAcoustIDClient(server.Client(), server.URL, "app-key", "EarthlyAudio/test")

			_, err := client.Lookup(context.Background(), identification.Fingerprint{DurationSeconds: 10, Value: "x"})

			if !errors.Is(err, identification.ErrServiceUnavailable) {
				t.Fatalf("error = %v, want ErrServiceUnavailable", err)
			}
		})
	}
}

func TestAcoustIDLookupHonoursContextDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{"status":"ok","results":[]}`))
	}))
	defer server.Close()
	client := identification.NewAcoustIDClient(server.Client(), server.URL, "app-key", "EarthlyAudio/test")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Lookup(ctx, identification.Fingerprint{DurationSeconds: 10, Value: "x"})

	if !errors.Is(err, identification.ErrServiceUnavailable) {
		t.Fatalf("error = %v, want ErrServiceUnavailable", err)
	}
}

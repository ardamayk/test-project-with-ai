package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/api"
	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/dependencies"
)

func TestGetHealth(t *testing.T) {
	h := api.NewHandler(config.Config{Version: "0.1.0-test"}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()

	h.GetHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Status       string   `json:"status"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("status field = %q", body.Status)
	}
	if len(body.Capabilities) == 0 || body.Capabilities[0] != "api.v1" {
		t.Fatalf("capabilities = %v, want api.v1 first", body.Capabilities)
	}
	if !slices.Contains(body.Capabilities, "playback.queue-events.v1") {
		t.Fatalf("capabilities = %v, want playback.queue-events.v1", body.Capabilities)
	}
	if !slices.Contains(body.Capabilities, "managed-import.v1") {
		t.Fatalf("capabilities = %v, want managed-import.v1", body.Capabilities)
	}
	if !slices.Contains(body.Capabilities, "managed-import-batches.v1") {
		t.Fatalf("capabilities = %v, want managed-import-batches.v1", body.Capabilities)
	}
	if !slices.Contains(body.Capabilities, "managed-track-deletion.v1") {
		t.Fatalf("capabilities = %v, want managed-track-deletion.v1", body.Capabilities)
	}
	if !slices.Contains(body.Capabilities, "managed-track-replacement.v1") {
		t.Fatalf("capabilities = %v, want managed-track-replacement.v1", body.Capabilities)
	}
	if !slices.Contains(body.Capabilities, "managed-album-deletion.v1") {
		t.Fatalf("capabilities = %v, want managed-album-deletion.v1", body.Capabilities)
	}
	if slices.Contains(body.Capabilities, "recording-identification.v1") {
		t.Fatalf("capabilities = %v, must not advertise recording-identification.v1 before Managed Import identifies recordings", body.Capabilities)
	}
}

func TestGetHealthReportsServerDependenciesAndRecordingIdentification(t *testing.T) {
	cfg := config.Config{Version: "0.1.0-test"}
	cfg.RecordingIdentification.Enabled = true
	cfg.RecordingIdentification.AcoustIDAPIKey = "key"
	cfg.RecordingIdentification.AcoustIDAPIKeySource = config.ACOUSTID_API_KEY_SOURCE_OPERATOR
	report := dependencies.Report{
		{Name: "ffmpeg", Required: true, Available: true, Version: "7.1"},
		{Name: "ffprobe", Required: true, Available: true, Version: "7.1"},
		{Name: "fpcalc", Required: false, Available: false},
	}
	h := api.NewHandler(cfg, report)
	rec := httptest.NewRecorder()

	h.GetHealth(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	var body struct {
		Dependencies []struct {
			Name      string `json:"name"`
			Required  bool   `json:"required"`
			Available bool   `json:"available"`
			Version   string `json:"version"`
		} `json:"dependencies"`
		RecordingIdentification struct {
			Status            string `json:"status"`
			AcoustIDKeySource string `json:"acoustIdKeySource"`
		} `json:"recordingIdentification"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Dependencies) != 3 || body.Dependencies[0].Name != "ffmpeg" || body.Dependencies[0].Version != "7.1" || !body.Dependencies[0].Required {
		t.Fatalf("dependencies = %+v", body.Dependencies)
	}
	if body.Dependencies[2].Name != "fpcalc" || body.Dependencies[2].Available || body.Dependencies[2].Required {
		t.Fatalf("fpcalc = %+v", body.Dependencies[2])
	}
	if body.RecordingIdentification.Status != "missing_fpcalc" {
		t.Fatalf("status = %q, want missing_fpcalc", body.RecordingIdentification.Status)
	}
	if body.RecordingIdentification.AcoustIDKeySource != "operator" {
		t.Fatalf("acoustIdKeySource = %q", body.RecordingIdentification.AcoustIDKeySource)
	}
}

func TestGetHealthRecordingIdentificationStatusPrecedence(t *testing.T) {
	available := dependencies.Report{{Name: "ffmpeg", Available: true}, {Name: "ffprobe", Available: true}, {Name: "fpcalc", Available: true}}
	cases := []struct {
		name    string
		enabled bool
		apiKey  string
		report  dependencies.Report
		want    string
	}{
		{"enabled", true, "key", available, "enabled"},
		{"disabled by config wins over missing fpcalc", false, "", dependencies.Report{{Name: "fpcalc"}}, "disabled_by_config"},
		{"missing fpcalc wins over missing key", true, "", dependencies.Report{{Name: "fpcalc"}}, "missing_fpcalc"},
		{"missing key", true, "", available, "missing_api_key"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := config.Config{}
			cfg.RecordingIdentification.Enabled = testCase.enabled
			cfg.RecordingIdentification.AcoustIDAPIKey = testCase.apiKey
			rec := httptest.NewRecorder()
			api.NewHandler(cfg, testCase.report).GetHealth(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
			var body struct {
				RecordingIdentification struct {
					Status string `json:"status"`
				} `json:"recordingIdentification"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.RecordingIdentification.Status != testCase.want {
				t.Fatalf("status = %q, want %q", body.RecordingIdentification.Status, testCase.want)
			}
		})
	}
}

func TestGetMe(t *testing.T) {
	h := api.NewHandler(config.Config{Version: "0.1.0-test"}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()

	h.GetMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["username"] != "admin" {
		t.Fatalf("username = %q", body["username"])
	}
}

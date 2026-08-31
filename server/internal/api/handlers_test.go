package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/api"
	"github.com/ardam/navidrome-replacement/server/internal/config"
)

func TestGetHealth(t *testing.T) {
	h := api.NewHandler(config.Config{Version: "0.1.0-test"})
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
}

func TestGetMe(t *testing.T) {
	h := api.NewHandler(config.Config{Version: "0.1.0-test"})
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

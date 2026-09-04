package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStreamAwareTimeoutSkipsStreamingRoutes(t *testing.T) {
	var streamHasDeadline bool
	var radioStationHasDeadline bool
	var radioPreviewHasDeadline bool
	var queueEventsHasDeadline bool
	var importUploadHasDeadline bool
	var permanentDeletionHasDeadline bool
	var apiHasDeadline bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasDeadline := r.Context().Deadline()
		if r.URL.Path == "/api/v1/tracks/track-1/stream" {
			streamHasDeadline = hasDeadline
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/api/v1/radio/preview/station-1/stream" {
			radioPreviewHasDeadline = hasDeadline
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/api/v1/radio/stations/station-1/stream" {
			radioStationHasDeadline = hasDeadline
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/api/v1/playback/queue/events" {
			queueEventsHasDeadline = hasDeadline
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/api/v1/imports/import-1/file" {
			importUploadHasDeadline = hasDeadline
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodDelete {
			permanentDeletionHasDeadline = hasDeadline
			w.WriteHeader(http.StatusOK)
			return
		}
		apiHasDeadline = hasDeadline
		w.WriteHeader(http.StatusOK)
	})
	wrapped := streamAwareTimeout(60 * time.Second)(handler)

	wrapped.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/api/v1/tracks/track-1/stream", nil),
	)
	permanentDeletionRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/library/tracks/track-1", nil)
	permanentDeletionRequest.Header.Set("X-Permanent-Delete", "1")
	wrapped.ServeHTTP(httptest.NewRecorder(), permanentDeletionRequest)
	wrapped.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/api/v1/radio/preview/station-1/stream", nil),
	)
	wrapped.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream", nil),
	)
	wrapped.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/api/v1/playback/queue/events", nil),
	)
	wrapped.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPut, "/api/v1/imports/import-1/file", nil),
	)
	wrapped.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/api/v1/health", nil),
	)

	if streamHasDeadline {
		t.Fatal("stream route should not inherit request timeout deadline")
	}
	if radioPreviewHasDeadline {
		t.Fatal("Radio Catalog Preview stream should not inherit request timeout deadline")
	}
	if radioStationHasDeadline {
		t.Fatal("Radio Station stream should not inherit request timeout deadline")
	}
	if queueEventsHasDeadline {
		t.Fatal("Queue event stream should not inherit request timeout deadline")
	}
	if importUploadHasDeadline {
		t.Fatal("Managed Import upload should not inherit request timeout deadline")
	}
	if permanentDeletionHasDeadline {
		t.Fatal("Permanent Track Deletion should not inherit request timeout deadline")
	}
	if !apiHasDeadline {
		t.Fatal("non-stream API route should keep request timeout deadline")
	}
}

func TestNewHTTPServerLeavesWriteTimeoutDisabledForStreams(t *testing.T) {
	server := newHTTPServer(":0", http.NewServeMux())
	if server.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout = %s, want disabled for long-running streams", server.WriteTimeout)
	}
}

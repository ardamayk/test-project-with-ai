package library

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/ardam/navidrome-replacement/server/internal/api/respond"
	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	service *Service
}

func NewHandlers(service *Service) *Handlers {
	return &Handlers{service: service}
}

// LEGACY_SCAN_RETIRED_CODE is returned by the deprecated scan trigger so older
// Playback Clients fail clearly instead of waiting for a scan that never runs.
const LEGACY_SCAN_RETIRED_CODE = "legacy_scan_retired"

// ScanStatus is the deprecated API v1 scan status shape. The scanner is
// retired, so the status is always idle.
type ScanStatus struct {
	Status  string `json:"status"`
	Scanned int    `json:"scanned"`
	Added   int    `json:"added"`
	Updated int    `json:"updated"`
	Removed int    `json:"removed"`
}

// TriggerScan answers the deprecated legacy scan trigger. It never discovers
// or ingests files: Managed Import is the authoritative ingestion path.
func (h *Handlers) TriggerScan(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Deprecation", "true")
	respond.Error(w, http.StatusGone, LEGACY_SCAN_RETIRED_CODE,
		"legacy library scanning is retired; add music through Managed Import or an explicit Library Migration")
}

// GetScanStatus answers the deprecated legacy scan status poll with a
// permanently idle status so older clients stop polling gracefully.
func (h *Handlers) GetScanStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Deprecation", "true")
	respond.JSON(w, http.StatusOK, ScanStatus{Status: "idle"})
}

func (h *Handlers) ListArtists(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagination(r)
	q := r.URL.Query().Get("q")
	result, err := h.service.ListArtists(r.Context(), limit, offset, q)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, result)
}

func (h *Handlers) ListAlbums(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagination(r)
	q := r.URL.Query().Get("q")
	artistID := r.URL.Query().Get("artistId")
	result, err := h.service.ListAlbums(r.Context(), limit, offset, artistID, q)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, result)
}

func (h *Handlers) GetAlbumCover(w http.ResponseWriter, r *http.Request) {
	albumID := chi.URLParam(r, "albumId")
	mime, data, err := h.service.GetAlbumCover(r.Context(), albumID)
	if errors.Is(err, ErrNotFound) {
		respond.Error(w, http.StatusNotFound, "not_found", "album cover not found")
		return
	}
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *Handlers) GetAlbum(w http.ResponseWriter, r *http.Request) {
	albumID := chi.URLParam(r, "albumId")
	result, err := h.service.GetAlbum(r.Context(), albumID)
	if errors.Is(err, ErrNotFound) {
		respond.Error(w, http.StatusNotFound, "not_found", "album not found")
		return
	}
	if errors.Is(err, ErrManagedAlbum) {
		respond.Error(w, http.StatusConflict, "managed_album_requires_track_deletion", err.Error())
		return
	}
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, result)
}

func (h *Handlers) ListTracks(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagination(r)
	q := r.URL.Query().Get("q")
	result, err := h.service.ListTracks(r.Context(), limit, offset, q)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, result)
}

func (h *Handlers) GetTrack(w http.ResponseWriter, r *http.Request) {
	trackID := chi.URLParam(r, "trackId")
	result, err := h.service.GetTrack(r.Context(), trackID)
	if errors.Is(err, ErrNotFound) {
		respond.Error(w, http.StatusNotFound, "not_found", "track not found")
		return
	}
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, result)
}

func (h *Handlers) DeleteAlbum(w http.ResponseWriter, r *http.Request) {
	albumID := chi.URLParam(r, "albumId")
	result, err := h.service.DeleteAlbum(r.Context(), albumID)
	if errors.Is(err, ErrNotFound) {
		respond.Error(w, http.StatusNotFound, "not_found", "album not found")
		return
	}
	if errors.Is(err, ErrMigrationStaged) {
		respond.Error(w, http.StatusConflict, "migration_staged", err.Error())
		return
	}
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, result)
}

func (h *Handlers) DeleteTrack(w http.ResponseWriter, r *http.Request) {
	trackID := chi.URLParam(r, "trackId")
	result, err := h.service.DeleteTrack(r.Context(), trackID)
	if errors.Is(err, ErrNotFound) {
		respond.Error(w, http.StatusNotFound, "not_found", "track not found")
		return
	}
	if errors.Is(err, ErrMigrationStaged) {
		respond.Error(w, http.StatusConflict, "migration_staged", err.Error())
		return
	}
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, result)
}

func pagination(r *http.Request) (limit, offset int) {
	limit = 50
	offset = 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

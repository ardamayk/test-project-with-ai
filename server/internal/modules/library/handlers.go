package library

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	service *Service
}

func NewHandlers(service *Service) *Handlers {
	return &Handlers{service: service}
}

func (h *Handlers) TriggerScan(w http.ResponseWriter, r *http.Request) {
	if !h.service.MusicPathsConfigured() {
		writeError(w, http.StatusBadRequest, "bad_request", "MUSIC_PATHS is not configured")
		return
	}

	st, err := h.service.TriggerScan(r.Context())
	if errors.Is(err, ErrScanRunning) {
		writeError(w, http.StatusConflict, "conflict", "scan already running")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, st)
}

func (h *Handlers) GetScanStatus(w http.ResponseWriter, r *http.Request) {
	st, err := h.service.GetScanStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *Handlers) ListArtists(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagination(r)
	q := r.URL.Query().Get("q")
	result, err := h.service.ListArtists(r.Context(), limit, offset, q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) ListAlbums(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagination(r)
	q := r.URL.Query().Get("q")
	artistID := r.URL.Query().Get("artistId")
	result, err := h.service.ListAlbums(r.Context(), limit, offset, artistID, q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) GetAlbum(w http.ResponseWriter, r *http.Request) {
	albumID := chi.URLParam(r, "albumId")
	result, err := h.service.GetAlbum(r.Context(), albumID)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "album not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) ListTracks(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagination(r)
	q := r.URL.Query().Get("q")
	result, err := h.service.ListTracks(r.Context(), limit, offset, q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) GetTrack(w http.ResponseWriter, r *http.Request) {
	trackID := chi.URLParam(r, "trackId")
	result, err := h.service.GetTrack(r.Context(), trackID)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "track not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{
		"error":   code,
		"code":    code,
		"message": message,
	})
}

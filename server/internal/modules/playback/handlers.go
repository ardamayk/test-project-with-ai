package playback

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ardam/navidrome-replacement/server/internal/api/respond"
	"github.com/ardam/navidrome-replacement/server/internal/auth"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	store   *Store
	library *library.Service
}

func NewHandlers(store *Store, libService *library.Service) *Handlers {
	return &Handlers{store: store, library: libService}
}

func (h *Handlers) GetQueue(w http.ResponseWriter, r *http.Request) {
	queue, err := h.store.GetQueue(r.Context(), auth.DefaultUserID)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, queue)
}

func (h *Handlers) ReplaceQueue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackIDs []string `json:"trackIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	queue, err := h.store.ReplaceQueue(r.Context(), auth.DefaultUserID, body.TrackIDs)
	if errors.Is(err, library.ErrNotFound) {
		respond.Error(w, http.StatusNotFound, "not_found", "track not found")
		return
	}
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, queue)
}

func (h *Handlers) AppendItem(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackID string `json:"trackId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if body.TrackID == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "trackId is required")
		return
	}

	queue, err := h.store.AppendItem(r.Context(), auth.DefaultUserID, body.TrackID)
	if errors.Is(err, library.ErrNotFound) {
		respond.Error(w, http.StatusNotFound, "not_found", "track not found")
		return
	}
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, queue)
}

func (h *Handlers) RemoveItem(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	queue, err := h.store.RemoveItem(r.Context(), auth.DefaultUserID, itemID)
	if errors.Is(err, ErrNotFound) {
		respond.Error(w, http.StatusNotFound, "not_found", "queue item not found")
		return
	}
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, queue)
}

func (h *Handlers) StreamTrack(w http.ResponseWriter, r *http.Request) {
	trackID := chi.URLParam(r, "trackId")
	filePath, err := h.library.GetTrackFilePath(r.Context(), trackID)
	if errors.Is(err, library.ErrNotFound) {
		respond.Error(w, http.StatusNotFound, "not_found", "track not found")
		return
	}
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if err := ServeTrackFile(w, r, filePath); err != nil {
		if errors.Is(err, ErrNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "track file not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
	}
}

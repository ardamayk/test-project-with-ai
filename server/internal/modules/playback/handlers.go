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
	store  *Store
	tracks library.TrackAccess
}

func NewHandlers(store *Store, tracks library.TrackAccess) *Handlers {
	return &Handlers{store: store, tracks: tracks}
}

func (h *Handlers) GetQueue(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.CurrentUserID(r)
	if err != nil {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	queue, err := h.store.GetQueue(r.Context(), userID)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, queue)
}

func (h *Handlers) ReplaceQueue(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.CurrentUserID(r)
	if err != nil {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	var body struct {
		TrackIDs []string `json:"trackIds"`
		Revision string   `json:"revision"`
	}
	if decodeErr := json.NewDecoder(r.Body).Decode(&body); decodeErr != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	if body.Revision == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "revision is required")
		return
	}

	queue, err := h.store.ReplaceQueue(r.Context(), userID, body.TrackIDs, body.Revision)
	if errors.Is(err, ErrRevisionConflict) {
		h.queueConflict(w, r, userID)
		return
	}
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
	userID, err := auth.CurrentUserID(r)
	if err != nil {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	var body struct {
		TrackID  string `json:"trackId"`
		Revision string `json:"revision"`
	}
	if decodeErr := json.NewDecoder(r.Body).Decode(&body); decodeErr != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if body.TrackID == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "trackId is required")
		return
	}
	if body.Revision == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "revision is required")
		return
	}

	queue, err := h.store.AppendItem(r.Context(), userID, body.TrackID, body.Revision)
	if errors.Is(err, ErrRevisionConflict) {
		h.queueConflict(w, r, userID)
		return
	}
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
	userID, err := auth.CurrentUserID(r)
	if err != nil {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	itemID := chi.URLParam(r, "itemId")
	revision := r.Header.Get("If-Match")
	if revision == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "If-Match revision is required")
		return
	}
	queue, err := h.store.RemoveItem(r.Context(), userID, itemID, revision)
	if errors.Is(err, ErrRevisionConflict) {
		h.queueConflict(w, r, userID)
		return
	}
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

func (h *Handlers) ReorderQueue(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.CurrentUserID(r)
	if err != nil {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	var body struct {
		ItemIDs  []string `json:"itemIds"`
		Revision string   `json:"revision"`
	}
	if decodeErr := json.NewDecoder(r.Body).Decode(&body); decodeErr != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if body.ItemIDs == nil || body.Revision == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "itemIds and revision are required")
		return
	}
	queue, err := h.store.ReorderItems(r.Context(), userID, body.ItemIDs, body.Revision)
	if errors.Is(err, ErrRevisionConflict) {
		h.queueConflict(w, r, userID)
		return
	}
	if errors.Is(err, ErrInvalidQueueOrder) {
		respond.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, queue)
}

func (h *Handlers) queueConflict(w http.ResponseWriter, r *http.Request, userID string) {
	queue, err := h.store.GetQueue(r.Context(), userID)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusConflict, struct {
		Error   string `json:"error"`
		Code    string `json:"code"`
		Message string `json:"message"`
		Queue   Queue  `json:"queue"`
	}{
		Error:   "conflict",
		Code:    "queue_revision_conflict",
		Message: "queue changed since supplied revision",
		Queue:   queue,
	})
}

func (h *Handlers) StreamTrack(w http.ResponseWriter, r *http.Request) {
	trackID := chi.URLParam(r, "trackId")
	filePath, err := h.tracks.GetTrackFilePath(r.Context(), trackID)
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

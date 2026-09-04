package api

import (
	"net/http"

	"github.com/ardam/navidrome-replacement/server/internal/api/respond"
	"github.com/ardam/navidrome-replacement/server/internal/auth"
	"github.com/ardam/navidrome-replacement/server/internal/config"
)

type Handler struct {
	version string
}

func NewHandler(cfg config.Config) *Handler {
	return &Handler{version: cfg.Version}
}

type healthResponse struct {
	Status       string   `json:"status"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
}

// serverCapabilities are the named behaviors this release advertises to
// separately released Playback Clients (ADR 0006). Every entry must be
// documented in the HealthResponse contract; clients ignore unknown entries.
var serverCapabilities = []string{
	"api.v1",
	"playback.queue-events.v1",
	"managed-import.v1",
	"managed-import-batches.v1",
	"managed-track-deletion.v1",
	"managed-track-replacement.v1",
	"managed-album-deletion.v1",
}

// ServerCapabilities returns a copy of the advertised Server Capabilities.
func ServerCapabilities() []string {
	return append([]string(nil), serverCapabilities...)
}

type userResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName,omitempty"`
}

func (h *Handler) GetHealth(w http.ResponseWriter, _ *http.Request) {
	respond.JSON(w, http.StatusOK, healthResponse{
		Status:       "ok",
		Version:      h.version,
		Capabilities: serverCapabilities,
	})
}

func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	user, err := auth.CurrentUser(r)
	if err != nil {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, userResponse{
		ID:          user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
	})
}

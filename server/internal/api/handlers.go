package api

import (
	"net/http"

	"github.com/ardam/navidrome-replacement/server/internal/api/respond"
	"github.com/ardam/navidrome-replacement/server/internal/auth"
	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/dependencies"
)

type Handler struct {
	version      string
	dependencies dependencies.Report
}

// NewHandler builds the health and identity handlers. The Server Dependency
// report is probed once at startup and echoed unchanged on every health call.
func NewHandler(cfg config.Config, report dependencies.Report) *Handler {
	return &Handler{
		version:      cfg.Version,
		dependencies: report,
	}
}

type healthResponse struct {
	Status       string               `json:"status"`
	Version      string               `json:"version"`
	Capabilities []string             `json:"capabilities"`
	Dependencies []dependencyResponse `json:"dependencies"`
}

type dependencyResponse struct {
	Name      string `json:"name"`
	Required  bool   `json:"required"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
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
	reported := make([]dependencyResponse, 0, len(h.dependencies))
	for _, dependency := range h.dependencies {
		reported = append(reported, dependencyResponse{
			Name:      dependency.Name,
			Required:  dependency.Required,
			Available: dependency.Available,
			Version:   dependency.Version,
		})
	}
	respond.JSON(w, http.StatusOK, healthResponse{
		Status:       "ok",
		Version:      h.version,
		Capabilities: serverCapabilities,
		Dependencies: reported,
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

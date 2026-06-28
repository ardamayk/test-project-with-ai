package api

import (
	"encoding/json"
	"net/http"

	"github.com/ardam/navidrome-replacement/server/internal/config"
)

type Handler struct {
	version string
}

func NewHandler(cfg config.Config) *Handler {
	return &Handler{version: cfg.Version}
}

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type userResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName,omitempty"`
}

func (h *Handler) GetHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Version: h.version})
}

func (h *Handler) GetMe(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, userResponse{
		ID:          "00000000-0000-0000-0000-000000000001",
		Username:    "admin",
		DisplayName: "Admin",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

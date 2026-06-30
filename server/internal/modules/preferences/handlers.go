package preferences

import (
	"encoding/json"
	"net/http"

	"github.com/ardam/navidrome-replacement/server/internal/api/respond"
	"github.com/ardam/navidrome-replacement/server/internal/auth"
)

func (m *Module) handleGet(w http.ResponseWriter, r *http.Request) {
	prefs, err := m.store.Get(r.Context(), auth.DefaultUserID)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, prefs)
}

func (m *Module) handlePatch(w http.ResponseWriter, r *http.Request) {
	var patch UserPreferences
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	prefs, err := m.store.Patch(r.Context(), auth.DefaultUserID, patch)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, prefs)
}

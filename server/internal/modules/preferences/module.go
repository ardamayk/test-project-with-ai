package preferences

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

const defaultUserID = "00000000-0000-0000-0000-000000000001"

type LayoutPreferences struct {
	SidebarPosition string              `json:"sidebarPosition"`
	Panels          map[string][]string `json:"panels"`
	Collapsed       map[string]bool     `json:"collapsed"`
}

type UserPreferences struct {
	Theme  string            `json:"theme"`
	Layout LayoutPreferences `json:"layout"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Get(ctx context.Context, userID string) (UserPreferences, error) {
	var theme, layoutJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT theme, layout_json FROM user_preferences WHERE user_id = ?`,
		userID,
	).Scan(&theme, &layoutJSON)
	if err != nil {
		return UserPreferences{}, fmt.Errorf("get preferences: %w", err)
	}

	var layout LayoutPreferences
	if err := json.Unmarshal([]byte(layoutJSON), &layout); err != nil {
		return UserPreferences{}, fmt.Errorf("decode layout: %w", err)
	}

	return UserPreferences{Theme: theme, Layout: layout}, nil
}

func (s *Store) Patch(ctx context.Context, userID string, patch UserPreferences) (UserPreferences, error) {
	current, err := s.Get(ctx, userID)
	if err != nil {
		return UserPreferences{}, err
	}

	if patch.Theme != "" {
		current.Theme = patch.Theme
	}
	if patch.Layout.SidebarPosition != "" {
		current.Layout.SidebarPosition = patch.Layout.SidebarPosition
	}
	if patch.Layout.Panels != nil {
		if patch.Layout.Panels["left"] != nil {
			current.Layout.Panels["left"] = patch.Layout.Panels["left"]
		}
		if patch.Layout.Panels["right"] != nil {
			current.Layout.Panels["right"] = patch.Layout.Panels["right"]
		}
	}
	if patch.Layout.Collapsed != nil {
		if _, ok := patch.Layout.Collapsed["left"]; ok {
			current.Layout.Collapsed["left"] = patch.Layout.Collapsed["left"]
		}
		if _, ok := patch.Layout.Collapsed["right"]; ok {
			current.Layout.Collapsed["right"] = patch.Layout.Collapsed["right"]
		}
	}

	layoutJSON, err := json.Marshal(current.Layout)
	if err != nil {
		return UserPreferences{}, fmt.Errorf("encode layout: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE user_preferences SET theme = ?, layout_json = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,
		current.Theme, string(layoutJSON), userID,
	)
	if err != nil {
		return UserPreferences{}, fmt.Errorf("update preferences: %w", err)
	}

	return current, nil
}

type Module struct {
	store *Store
}

func NewModule(store *Store) *Module {
	return &Module{store: store}
}

func (m *Module) Name() string {
	return "preferences"
}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/preferences", m.handleGet)
	r.Patch("/api/v1/preferences", m.handlePatch)
}

func (m *Module) handleGet(w http.ResponseWriter, r *http.Request) {
	prefs, err := m.store.Get(r.Context(), defaultUserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (m *Module) handlePatch(w http.ResponseWriter, r *http.Request) {
	var patch UserPreferences
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	prefs, err := m.store.Patch(r.Context(), defaultUserID, patch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, prefs)
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

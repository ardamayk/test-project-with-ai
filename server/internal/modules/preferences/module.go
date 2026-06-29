package preferences

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ardam/navidrome-replacement/server/internal/auth"
	"github.com/go-chi/chi/v5"
)

type ThemePreferences struct {
	Mode   string `json:"mode"`
	Preset string `json:"preset"`
}

type LayoutPreferences struct {
	SidebarPosition string              `json:"sidebarPosition"`
	Panels          map[string][]string `json:"panels"`
	Collapsed       map[string]bool     `json:"collapsed"`
	Sizes           []float64           `json:"sizes,omitempty"`
}

type UserPreferences struct {
	Theme  ThemePreferences  `json:"theme"`
	Layout LayoutPreferences `json:"layout"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func defaultTheme() ThemePreferences {
	return ThemePreferences{Mode: "system", Preset: "earthly"}
}

func defaultLayout() LayoutPreferences {
	return LayoutPreferences{
		SidebarPosition: "left",
		Panels: map[string][]string{
			"left":  {"now-playing"},
			"right": {"discover"},
		},
		Collapsed: map[string]bool{"left": false, "right": false},
		Sizes:     []float64{22, 50, 28},
	}
}

func parseThemeColumn(raw string) ThemePreferences {
	var theme ThemePreferences
	if err := json.Unmarshal([]byte(raw), &theme); err == nil && theme.Mode != "" {
		if theme.Preset == "" {
			theme.Preset = "earthly"
		}
		return theme
	}
	switch raw {
	case "light", "dark", "system":
		return ThemePreferences{Mode: raw, Preset: "earthly"}
	default:
		return defaultTheme()
	}
}

func themeToColumn(theme ThemePreferences) string {
	if theme.Mode == "" {
		theme = defaultTheme()
	}
	if theme.Preset == "" {
		theme.Preset = "earthly"
	}
	b, _ := json.Marshal(theme)
	return string(b)
}

func clampPanelSizes(sizes []float64, collapsed map[string]bool) []float64 {
	const minLeft, maxLeft = 15.0, 45.0
	const minRight, maxRight = 18.0, 50.0
	const minMini, maxMini = 4.0, 12.0
	const minMain = 25.0

	left := sizes[0]
	right := sizes[2]

	if collapsed != nil && collapsed["left"] {
		if left < minMini {
			left = minMini
		}
		if left > maxMini {
			left = maxMini
		}
	} else {
		if left < minLeft {
			left = minLeft
		}
		if left > maxLeft {
			left = maxLeft
		}
	}

	if collapsed != nil && collapsed["right"] {
		if right < minMini {
			right = minMini
		}
		if right > maxMini {
			right = maxMini
		}
	} else {
		if right < minRight {
			right = minRight
		}
		if right > maxRight {
			right = maxRight
		}
	}

	main := 100 - left - right
	if main < minMain {
		deficit := minMain - main
		sideSum := left + right
		left -= (left / sideSum) * deficit
		right -= (right / sideSum) * deficit
		main = minMain
	}
	return []float64{left, main, right}
}

func normalizeLayout(layout LayoutPreferences) LayoutPreferences {
	if layout.SidebarPosition == "" {
		layout.SidebarPosition = "left"
	}
	if layout.Panels == nil {
		layout.Panels = defaultLayout().Panels
	}
	if layout.Collapsed == nil {
		layout.Collapsed = defaultLayout().Collapsed
	}
	if len(layout.Sizes) != 3 {
		layout.Sizes = defaultLayout().Sizes
	} else {
		layout.Sizes = clampPanelSizes(layout.Sizes, layout.Collapsed)
	}
	return layout
}

func (s *Store) Get(ctx context.Context, userID string) (UserPreferences, error) {
	var themeRaw, layoutJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT theme, layout_json FROM user_preferences WHERE user_id = ?`,
		userID,
	).Scan(&themeRaw, &layoutJSON)
	if err != nil {
		return UserPreferences{}, fmt.Errorf("get preferences: %w", err)
	}

	var layout LayoutPreferences
	if err := json.Unmarshal([]byte(layoutJSON), &layout); err != nil {
		return UserPreferences{}, fmt.Errorf("decode layout: %w", err)
	}

	return UserPreferences{
		Theme:  parseThemeColumn(themeRaw),
		Layout: normalizeLayout(layout),
	}, nil
}

func (s *Store) Patch(ctx context.Context, userID string, patch UserPreferences) (UserPreferences, error) {
	current, err := s.Get(ctx, userID)
	if err != nil {
		return UserPreferences{}, err
	}

	if patch.Theme.Mode != "" {
		current.Theme.Mode = patch.Theme.Mode
	}
	if patch.Theme.Preset != "" {
		current.Theme.Preset = patch.Theme.Preset
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
	if len(patch.Layout.Sizes) == 3 {
		current.Layout.Sizes = patch.Layout.Sizes
	}

	current.Layout = normalizeLayout(current.Layout)

	layoutJSON, err := json.Marshal(current.Layout)
	if err != nil {
		return UserPreferences{}, fmt.Errorf("encode layout: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE user_preferences SET theme = ?, layout_json = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,
		themeToColumn(current.Theme), string(layoutJSON), userID,
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
	prefs, err := m.store.Get(r.Context(), auth.DefaultUserID)
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

	prefs, err := m.store.Patch(r.Context(), auth.DefaultUserID, patch)
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

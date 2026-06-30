package preferences

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
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

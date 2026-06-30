package preferences

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

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

// PlaybackPreferences tunes the Player Bar. Every field always carries a
// value once read from the store; the patch type below is the sparse form.
type PlaybackPreferences struct {
	SeekStepSeconds        int     `json:"seekStepSeconds"`
	SeekStepLargeSeconds   int     `json:"seekStepLargeSeconds"`
	PlaybackRate           float64 `json:"playbackRate"`
	TransitionFadeMs       int     `json:"transitionFadeMs"`
	ShowWaveform           bool    `json:"showWaveform"`
	AccentFromCover        bool    `json:"accentFromCover"`
	ShowUpNext             bool    `json:"showUpNext"`
	AutoSkipOnErrorSeconds int     `json:"autoSkipOnErrorSeconds"`
	HoverTimestamp         bool    `json:"hoverTimestamp"`
}

// PlaybackPreferencesPatch carries only the fields the client sent, so a
// false boolean can be distinguished from an absent one.
type PlaybackPreferencesPatch struct {
	SeekStepSeconds        *int     `json:"seekStepSeconds,omitempty"`
	SeekStepLargeSeconds   *int     `json:"seekStepLargeSeconds,omitempty"`
	PlaybackRate           *float64 `json:"playbackRate,omitempty"`
	TransitionFadeMs       *int     `json:"transitionFadeMs,omitempty"`
	ShowWaveform           *bool    `json:"showWaveform,omitempty"`
	AccentFromCover        *bool    `json:"accentFromCover,omitempty"`
	ShowUpNext             *bool    `json:"showUpNext,omitempty"`
	AutoSkipOnErrorSeconds *int     `json:"autoSkipOnErrorSeconds,omitempty"`
	HoverTimestamp         *bool    `json:"hoverTimestamp,omitempty"`
}

type UserPreferences struct {
	Theme    ThemePreferences    `json:"theme"`
	Layout   LayoutPreferences   `json:"layout"`
	Playback PlaybackPreferences `json:"playback"`
}

// UserPreferencesPatch is the PATCH body. Theme and Layout keep their
// zero-value-means-absent convention; Playback uses explicit pointers.
type UserPreferencesPatch struct {
	Theme    ThemePreferences          `json:"theme"`
	Layout   LayoutPreferences         `json:"layout"`
	Playback *PlaybackPreferencesPatch `json:"playback,omitempty"`
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

func defaultPlayback() PlaybackPreferences {
	return PlaybackPreferences{
		SeekStepSeconds:        5,
		SeekStepLargeSeconds:   30,
		PlaybackRate:           1,
		TransitionFadeMs:       0,
		ShowWaveform:           true,
		AccentFromCover:        true,
		ShowUpNext:             true,
		AutoSkipOnErrorSeconds: 5,
		HoverTimestamp:         true,
	}
}

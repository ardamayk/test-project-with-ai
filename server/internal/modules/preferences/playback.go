package preferences

import (
	"encoding/json"
	"fmt"
)

// Bounds for the numeric Playback Preferences; the handler rejects a patch
// outside them so the Player Bar never receives a value it cannot honor.
const (
	MinSeekStepSeconds        = 1
	MaxSeekStepSeconds        = 120
	MinPlaybackRate           = 0.5
	MaxPlaybackRate           = 2
	MinTransitionFadeMs       = 0
	MaxTransitionFadeMs       = 2000
	MinAutoSkipOnErrorSeconds = 0
	MaxAutoSkipOnErrorSeconds = 60
)

// parsePlaybackColumn decodes the stored JSON over the defaults so rows
// written before a field existed still read back complete.
func parsePlaybackColumn(raw string) PlaybackPreferences {
	playback := defaultPlayback()
	if raw == "" {
		return playback
	}
	var stored PlaybackPreferencesPatch
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return playback
	}
	applyPlaybackPatch(&playback, &stored)
	return playback
}

func applyPlaybackPatch(current *PlaybackPreferences, patch *PlaybackPreferencesPatch) {
	if patch == nil {
		return
	}
	if patch.SeekStepSeconds != nil {
		current.SeekStepSeconds = *patch.SeekStepSeconds
	}
	if patch.SeekStepLargeSeconds != nil {
		current.SeekStepLargeSeconds = *patch.SeekStepLargeSeconds
	}
	if patch.PlaybackRate != nil {
		current.PlaybackRate = *patch.PlaybackRate
	}
	if patch.TransitionFadeMs != nil {
		current.TransitionFadeMs = *patch.TransitionFadeMs
	}
	if patch.ShowWaveform != nil {
		current.ShowWaveform = *patch.ShowWaveform
	}
	if patch.AccentFromCover != nil {
		current.AccentFromCover = *patch.AccentFromCover
	}
	if patch.ShowUpNext != nil {
		current.ShowUpNext = *patch.ShowUpNext
	}
	if patch.AutoSkipOnErrorSeconds != nil {
		current.AutoSkipOnErrorSeconds = *patch.AutoSkipOnErrorSeconds
	}
	if patch.HoverTimestamp != nil {
		current.HoverTimestamp = *patch.HoverTimestamp
	}
}

// ValidatePlaybackPatch returns the first out-of-range field, or nil.
func ValidatePlaybackPatch(patch *PlaybackPreferencesPatch) error {
	if patch == nil {
		return nil
	}
	checkInt := func(field string, value *int, minimum, maximum int) error {
		if value != nil && (*value < minimum || *value > maximum) {
			return fmt.Errorf("%s must be between %d and %d", field, minimum, maximum)
		}
		return nil
	}
	if err := checkInt("seekStepSeconds", patch.SeekStepSeconds, MinSeekStepSeconds, MaxSeekStepSeconds); err != nil {
		return err
	}
	if err := checkInt("seekStepLargeSeconds", patch.SeekStepLargeSeconds, MinSeekStepSeconds, MaxSeekStepSeconds); err != nil {
		return err
	}
	if patch.PlaybackRate != nil && (*patch.PlaybackRate < MinPlaybackRate || *patch.PlaybackRate > MaxPlaybackRate) {
		return fmt.Errorf("playbackRate must be between %v and %v", MinPlaybackRate, MaxPlaybackRate)
	}
	if err := checkInt("transitionFadeMs", patch.TransitionFadeMs, MinTransitionFadeMs, MaxTransitionFadeMs); err != nil {
		return err
	}
	return checkInt("autoSkipOnErrorSeconds", patch.AutoSkipOnErrorSeconds, MinAutoSkipOnErrorSeconds, MaxAutoSkipOnErrorSeconds)
}

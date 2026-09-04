package library

import (
	"math"
	"strconv"
	"strings"
)

type ReplayGainMetadata struct {
	TrackGainDB *float64 `json:"trackGainDb"`
	TrackPeak   *float64 `json:"trackPeak"`
	AlbumGainDB *float64 `json:"albumGainDb"`
	AlbumPeak   *float64 `json:"albumPeak"`
}

func readReplayGainStringMetadata(raw map[string]string) ReplayGainMetadata {
	values := make(map[string]string, len(raw))
	for key, value := range raw {
		values[strings.ToUpper(key)] = value
	}
	return replayGainFromStringValues(values)
}

func replayGainFromStringValues(values map[string]string) ReplayGainMetadata {
	return ReplayGainMetadata{
		TrackGainDB: parseReplayGainValue(values["REPLAYGAIN_TRACK_GAIN"]),
		TrackPeak:   parseReplayGainPeak(values["REPLAYGAIN_TRACK_PEAK"]),
		AlbumGainDB: parseReplayGainValue(values["REPLAYGAIN_ALBUM_GAIN"]),
		AlbumPeak:   parseReplayGainPeak(values["REPLAYGAIN_ALBUM_PEAK"]),
	}
}

func parseReplayGainValue(value string) *float64 {
	value = strings.TrimSpace(value)
	value = strings.TrimSpace(strings.TrimSuffix(strings.ToLower(value), "db"))
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return nil
	}
	return &parsed
}

func parseReplayGainPeak(value string) *float64 {
	parsed := parseReplayGainValue(value)
	if parsed == nil || *parsed < 0 {
		return nil
	}
	return parsed
}

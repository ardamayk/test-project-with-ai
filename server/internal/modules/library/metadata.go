package library

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/dhowden/tag"
)

type FileMetadata struct {
	Path         string
	Format       string
	SizeBytes    int64
	ModTime      time.Time
	Title        string
	Artist       string
	AlbumArtist  string
	Album        string
	TrackNo      int
	Year         int
	DurationMs   int
	Genre        string
	SampleRateHz int
	BitDepth     int
	CoverMime    string
	CoverData    []byte
	ReplayGain   ReplayGainMetadata
}

type ReplayGainMetadata struct {
	TrackGainDB *float64 `json:"trackGainDb"`
	TrackPeak   *float64 `json:"trackPeak"`
	AlbumGainDB *float64 `json:"albumGainDb"`
	AlbumPeak   *float64 `json:"albumPeak"`
}

func readReplayGainMetadata(raw map[string]interface{}) ReplayGainMetadata {
	values := make(map[string]string, len(raw))
	for key, value := range raw {
		normalizedKey := strings.ToUpper(key)
		switch typedValue := value.(type) {
		case string:
			values[normalizedKey] = typedValue
		case []string:
			if len(typedValue) > 0 {
				values[normalizedKey] = typedValue[0]
			}
		case *tag.Comm:
			values[strings.ToUpper(typedValue.Description)] = typedValue.Text
		case tag.Comm:
			values[strings.ToUpper(typedValue.Description)] = typedValue.Text
		}
	}

	return replayGainFromStringValues(values)
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

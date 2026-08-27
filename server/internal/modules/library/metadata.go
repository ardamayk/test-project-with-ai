package library

import (
	"fmt"
	"math"
	"os"
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

func readFileMetadata(path string, format string, info os.FileInfo) (FileMetadata, error) {
	meta := FileMetadata{
		Path:      path,
		Format:    format,
		SizeBytes: info.Size(),
		ModTime:   info.ModTime(),
		Title:     fallbackTitle(path),
		Artist:    "Unknown Artist",
		Album:     "Unknown Album",
	}

	f, err := os.Open(path)
	if err != nil {
		return meta, fmt.Errorf("open file: %w", err)
	}
	defer func() { _ = f.Close() }()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return meta, nil
	}

	meta.Title = sanitizeName(m.Title(), meta.Title)
	meta.Artist = sanitizeName(m.Artist(), meta.Artist)
	meta.Album = sanitizeName(m.Album(), meta.Album)
	albumArtist := sanitizeName(m.AlbumArtist(), "")
	if albumArtist == "" {
		albumArtist = meta.Artist
	}
	meta.AlbumArtist = albumArtist
	if track, _ := m.Track(); track > 0 {
		meta.TrackNo = track
	}
	if year := m.Year(); year > 0 {
		meta.Year = year
	}
	meta.DurationMs = durationMsFromFile(path, format, m)
	if genres := splitGenres(m.Genre()); len(genres) > 0 {
		meta.Genre = genres[0]
	}
	meta.ReplayGain = readReplayGainMetadata(m.Raw())
	applyAudioFormat(&meta)
	if pic := m.Picture(); pic != nil && len(pic.Data) > 0 {
		meta.CoverMime = pic.MIMEType
		if meta.CoverMime == "" {
			meta.CoverMime = "image/jpeg"
		}
		meta.CoverData = pic.Data
	}

	return meta, nil
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

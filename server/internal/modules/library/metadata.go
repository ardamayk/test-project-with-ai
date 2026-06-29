package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhowden/tag"
)

type FileMetadata struct {
	Path        string
	Format      string
	SizeBytes   int64
	ModTime     time.Time
	Title       string
	Artist      string
	AlbumArtist string
	Album       string
	TrackNo     int
	Year        int
	DurationMs   int
	Genre        string
	SampleRateHz int
	BitDepth     int
	CoverMime    string
	CoverData    []byte
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
	defer f.Close()

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

func parseFilenameArtistAlbum(path string) (artist, album, title string) {
	base := filepath.Base(path)
	if idx := strings.LastIndex(base, "."); idx >= 0 {
		base = base[:idx]
	}
	parts := strings.Split(base, " - ")
	if len(parts) >= 3 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(strings.Join(parts[2:], " - "))
	}
	if len(parts) == 2 {
		return "Unknown Artist", strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "Unknown Artist", "Unknown Album", base
}

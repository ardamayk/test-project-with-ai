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
	Path       string
	Format     string
	SizeBytes  int64
	ModTime    time.Time
	Title      string
	Artist     string
	Album      string
	TrackNo    int
	Year       int
	DurationMs int
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
	if track, _ := m.Track(); track > 0 {
		meta.TrackNo = track
	}
	if year := m.Year(); year > 0 {
		meta.Year = year
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

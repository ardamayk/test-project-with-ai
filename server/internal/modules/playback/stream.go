package playback

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var contentTypes = map[string]string{
	".mp3":  "audio/mpeg",
	".flac": "audio/flac",
	".ogg":  "audio/ogg",
	".m4a":  "audio/mp4",
	".opus": "audio/opus",
	".wav":  "audio/wav",
}

func contentTypeForPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ct, ok := contentTypes[ext]; ok {
		return ct
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func ServeTrackFile(w http.ResponseWriter, r *http.Request, filePath string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}

	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	defer f.Close()

	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", contentTypeForPath(filePath))
	http.ServeContent(w, r, filepath.Base(filePath), info.ModTime(), f)
	return nil
}

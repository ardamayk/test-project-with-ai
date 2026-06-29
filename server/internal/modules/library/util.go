package library

import (
	"strings"
	"unicode"
)

var supportedExtensions = map[string]string{
	".mp3":  "mp3",
	".flac": "flac",
	".ogg":  "ogg",
	".m4a":  "m4a",
	".opus": "opus",
	".wav":  "wav",
}

func isSupportedFile(name string) (format string, ok bool) {
	lower := strings.ToLower(name)
	for ext, fmt := range supportedExtensions {
		if strings.HasSuffix(lower, ext) {
			return fmt, true
		}
	}
	return "", false
}

func sortKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func fallbackTitle(path string) string {
	base := path
	if idx := strings.LastIndexAny(path, `/\`); idx >= 0 {
		base = path[idx+1:]
	}
	if idx := strings.LastIndex(base, "."); idx >= 0 {
		base = base[:idx]
	}
	base = strings.ReplaceAll(base, "_", " ")
	return strings.TrimSpace(base)
}

func sanitizeName(name string, fallback string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return fallback
	}
	return name
}

func isLetterOrDigit(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

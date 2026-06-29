package library

import (
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

func durationMsFromFile(path, format string, m tag.Metadata) int {
	if format == "flac" {
		if info, ok := readFlacStreamInfo(path); ok && info.DurationMs > 0 {
			return info.DurationMs
		}
	}
	if ms := id3DurationMs(m); ms > 0 {
		return ms
	}
	return 0
}

func id3DurationMs(m tag.Metadata) int {
	raw := m.Raw()
	if raw == nil {
		return 0
	}
	for _, key := range []string{"TLEN", "Length"} {
		v, ok := raw[key]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case string:
			ms, err := strconv.Atoi(strings.TrimSpace(n))
			if err == nil && ms > 0 {
				return ms
			}
		case int:
			if n > 0 {
				return n
			}
		case int64:
			if n > 0 {
				return int(n)
			}
		}
	}
	return 0
}

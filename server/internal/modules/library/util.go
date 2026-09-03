package library

import (
	"strings"
)

func sortKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func sanitizeName(name string, fallback string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return fallback
	}
	return name
}

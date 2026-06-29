package library

import "testing"

func TestFallbackTitle(t *testing.T) {
	if got := fallbackTitle(`/music/Artist - Song_Name.flac`); got != "Artist - Song Name" {
		t.Fatalf("fallbackTitle = %q", got)
	}
	if got := fallbackTitle(""); got != "" {
		t.Fatalf("empty path = %q", got)
	}
}

func TestSanitizeName(t *testing.T) {
	if got := sanitizeName("  Taylor Swift  ", "fallback"); got != "Taylor Swift" {
		t.Fatalf("sanitizeName trim = %q", got)
	}
	if got := sanitizeName("   ", "Unknown"); got != "Unknown" {
		t.Fatalf("sanitizeName empty = %q", got)
	}
}

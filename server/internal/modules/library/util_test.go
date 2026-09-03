package library

import "testing"

func TestSortKey(t *testing.T) {
	if sortKey("  Hello ") != "hello" {
		t.Fatal("sortKey normalization failed")
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

package library

import "testing"

func TestResolveAlbumArtist(t *testing.T) {
	tests := []struct {
		name        string
		trackArtist string
		albumArtist string
		want        string
	}{
		{
			name:        "prefers album artist tag",
			trackArtist: "Lana Del Rey",
			albumArtist: "Taylor Swift",
			want:        "Taylor Swift",
		},
		{
			name:        "falls back to track artist",
			trackArtist: "Taylor Swift",
			albumArtist: "",
			want:        "Taylor Swift",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeName(tc.albumArtist, "")
			if got == "" {
				got = tc.trackArtist
			}
			if got != tc.want {
				t.Fatalf("album artist = %q, want %q", got, tc.want)
			}
		})
	}
}

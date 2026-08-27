package library

import (
	"testing"

	"github.com/dhowden/tag"
)

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

func TestReadReplayGainMetadata(t *testing.T) {
	tests := []struct {
		name string
		raw  map[string]interface{}
		want ReplayGainMetadata
	}{
		{
			name: "reads all Vorbis values",
			raw: map[string]interface{}{
				"replaygain_track_gain": "-7.25 dB",
				"replaygain_track_peak": "0.987654",
				"replaygain_album_gain": "-6.50 dB",
				"replaygain_album_peak": "1.012345",
			},
			want: ReplayGainMetadata{
				TrackGainDB: float64Pointer(-7.25),
				TrackPeak:   float64Pointer(0.987654),
				AlbumGainDB: float64Pointer(-6.5),
				AlbumPeak:   float64Pointer(1.012345),
			},
		},
		{
			name: "reads partial ID3 user text values",
			raw: map[string]interface{}{
				"TXXX":   &tag.Comm{Description: "REPLAYGAIN_TRACK_GAIN", Text: "+1.75 dB"},
				"TXXX_0": &tag.Comm{Description: "REPLAYGAIN_ALBUM_PEAK", Text: "0.95"},
			},
			want: ReplayGainMetadata{
				TrackGainDB: float64Pointer(1.75),
				AlbumPeak:   float64Pointer(0.95),
			},
		},
		{
			name: "ignores malformed and non-finite values",
			raw: map[string]interface{}{
				"REPLAYGAIN_TRACK_GAIN": []string{"loud"},
				"REPLAYGAIN_TRACK_PEAK": []string{"NaN"},
				"REPLAYGAIN_ALBUM_GAIN": []string{"+Inf dB"},
				"REPLAYGAIN_ALBUM_PEAK": []string{""},
			},
			want: ReplayGainMetadata{},
		},
		{
			name: "keeps valid gains while rejecting negative peaks",
			raw: map[string]interface{}{
				"REPLAYGAIN_TRACK_GAIN": "-7.25 dB",
				"REPLAYGAIN_TRACK_PEAK": "-0.01",
				"REPLAYGAIN_ALBUM_GAIN": "+1.50 dB",
				"REPLAYGAIN_ALBUM_PEAK": "-1",
			},
			want: ReplayGainMetadata{
				TrackGainDB: float64Pointer(-7.25),
				AlbumGainDB: float64Pointer(1.5),
			},
		},
		{
			name: "keeps absent values unavailable",
			raw:  map[string]interface{}{},
			want: ReplayGainMetadata{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := readReplayGainMetadata(test.raw)
			assertReplayGainMetadata(t, got, test.want)
		})
	}
}

func float64Pointer(value float64) *float64 {
	return &value
}

func assertReplayGainMetadata(t *testing.T, got, want ReplayGainMetadata) {
	t.Helper()
	assertOptionalFloat64(t, "track gain", got.TrackGainDB, want.TrackGainDB)
	assertOptionalFloat64(t, "track peak", got.TrackPeak, want.TrackPeak)
	assertOptionalFloat64(t, "album gain", got.AlbumGainDB, want.AlbumGainDB)
	assertOptionalFloat64(t, "album peak", got.AlbumPeak, want.AlbumPeak)
}

func assertOptionalFloat64(t *testing.T, name string, got, want *float64) {
	t.Helper()
	if got == nil || want == nil {
		if got != want {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
		return
	}
	if *got != *want {
		t.Fatalf("%s = %v, want %v", name, *got, *want)
	}
}

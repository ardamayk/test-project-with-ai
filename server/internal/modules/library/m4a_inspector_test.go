package library

import (
	"reflect"
	"testing"
)

func TestInspectM4AMetadataPreservesStructuredValues(t *testing.T) {
	metadata, err := inspectM4AMetadata(map[string]string{
		"title":                 "M4A Credits",
		"artist":                "Display Artist",
		"album_artist":          "Display Album Artist",
		"album":                 "Strict M4A Tests",
		"genre":                 "R&B; Pop/Rock|Live, Bootleg",
		"track":                 "3/9",
		"disc":                  "1/2",
		"date":                  "2026-08-31",
		"replaygain_track_gain": "-7.25 dB",
	}, map[string][]string{
		"ARTISTS":      {"Artist, One", "Artist & Two"},
		"ALBUMARTISTS": {"Album Artist / One", "Album Artist Two"},
	})
	if err != nil {
		t.Fatalf("inspect M4A metadata: %v", err)
	}
	if !reflect.DeepEqual(metadata.Artists, []string{"Artist, One", "Artist & Two"}) {
		t.Fatalf("structured Artists = %#v", metadata.Artists)
	}
	if !reflect.DeepEqual(metadata.AlbumArtists, []string{"Album Artist / One", "Album Artist Two"}) {
		t.Fatalf("structured Album Artists = %#v", metadata.AlbumArtists)
	}
	if !reflect.DeepEqual(metadata.Genres, []string{"R&B", "Pop", "Rock", "Live, Bootleg"}) {
		t.Fatalf("Genres = %#v", metadata.Genres)
	}
	if metadata.TrackPosition != (MediaPosition{Number: 3, Total: 9}) || metadata.DiscPosition != (MediaPosition{Number: 1, Total: 2}) || metadata.Year != 2026 {
		t.Fatalf("positions/year = %+v / %+v / %d", metadata.TrackPosition, metadata.DiscPosition, metadata.Year)
	}
	if metadata.ReplayGain.TrackGainDB == nil || *metadata.ReplayGain.TrackGainDB != -7.25 {
		t.Fatalf("ReplayGain = %+v", metadata.ReplayGain)
	}
}

func TestM4AProbeRequiresExactBrandToken(t *testing.T) {
	if isM4AProbe(m4aProbe{Format: m4aProbeFormat{FormatName: "mov,mp4,m4a", Tags: map[string]string{"major_brand": "XM4AX"}}}) {
		t.Fatal("substring-only M4A brand was accepted")
	}
	if !isM4AProbe(m4aProbe{Format: m4aProbeFormat{FormatName: "mov,mp4,m4a", Tags: map[string]string{"compatible_brands": "isomM4A iso2"}}}) {
		t.Fatal("exact compatible M4A brand was rejected")
	}
}

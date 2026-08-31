package library_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

func TestMediaInspectorInspectsStrictMP3Fixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strict-import.mp3")
	fixture := testutil.StrictMP3Fixture()
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatalf("write strict MP3 fixture: %v", err)
	}

	inspection, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("inspect strict MP3 fixture: %v", err)
	}
	if inspection.Metadata.Title != "MP3 Inspection Fixture" || inspection.Metadata.Album != "Strict MP3 Import Tests" {
		t.Fatalf("MP3 identity metadata = %+v", inspection.Metadata)
	}
	if !reflect.DeepEqual(inspection.Metadata.Artists, []string{"Primary Artist", "Guest Artist"}) ||
		!reflect.DeepEqual(inspection.Metadata.AlbumArtists, []string{"Album Artist"}) ||
		!reflect.DeepEqual(inspection.Metadata.Genres, []string{"Electronic", "Ambient"}) {
		t.Fatalf("MP3 structured metadata = %+v", inspection.Metadata)
	}
	if inspection.Metadata.TrackPosition != (library.MediaPosition{Number: 2, Total: 8}) ||
		inspection.Metadata.DiscPosition != (library.MediaPosition{Number: 1, Total: 1}) || inspection.Metadata.Year != 2026 {
		t.Fatalf("MP3 position/date metadata = %+v", inspection.Metadata)
	}
	if inspection.AlbumArtwork.MIMEType != "image/png" || inspection.AlbumArtwork.Width != 32 || inspection.AlbumArtwork.Height != 32 {
		t.Fatalf("MP3 Album Artwork = %+v", inspection.AlbumArtwork)
	}
	audio := inspection.Audio
	if audio.Format != "mp3" || audio.Container != "mp3" || audio.Codec != "mp3" {
		t.Fatalf("MP3 format/container/codec = %s/%s/%s", audio.Format, audio.Container, audio.Codec)
	}
	if audio.SampleRateHz != 22_050 || audio.ChannelCount != 1 || audio.BitDepth != 0 || audio.BitrateKbps != 52 || audio.DurationMs <= 0 {
		t.Fatalf("MP3 technical properties = %+v", audio)
	}
}

func TestMediaInspectorSupportsStrictID3Variants(t *testing.T) {
	for _, version := range []byte{2, 3, 4} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			inspection := inspectMP3Fixture(t, testutil.StrictMP3FixtureWithID3Version(version))
			if !reflect.DeepEqual(inspection.Metadata.Artists, []string{"Primary Artist", "Guest Artist"}) ||
				!reflect.DeepEqual(inspection.Metadata.Genres, []string{"Electronic", "Ambient"}) {
				t.Fatalf("ID3v2.%d structured metadata = %+v", version, inspection.Metadata)
			}
		})
	}
}

func TestMediaInspectorReadsMP3ReplayGain(t *testing.T) {
	replayGain := inspectMP3Fixture(t, testutil.StrictMP3Fixture()).Metadata.ReplayGain
	if replayGain.TrackGainDB == nil || *replayGain.TrackGainDB != -7.25 ||
		replayGain.TrackPeak == nil || *replayGain.TrackPeak != 0.9123 ||
		replayGain.AlbumGainDB == nil || *replayGain.AlbumGainDB != -6.75 ||
		replayGain.AlbumPeak == nil || *replayGain.AlbumPeak != 0.9789 {
		t.Fatalf("MP3 ReplayGain = %+v", replayGain)
	}
}

func TestMediaInspectorRejectsInvalidMP3TagsArtworkCodecAndTruncation(t *testing.T) {
	fixture := testutil.StrictMP3Fixture()
	audioOffset := strictID3AudioOffset(t, fixture)
	pngOffset := bytes.Index(fixture, []byte(library.PNG_SIGNATURE))
	if pngOffset < 0 {
		t.Fatal("strict MP3 fixture has no embedded PNG")
	}
	tests := []struct {
		name  string
		data  []byte
		code  library.InspectionErrorCode
		field string
	}{
		{name: "invalid tag version", data: mutateMP3Fixture(fixture, 3, 5), code: library.INSPECTION_ERROR_INVALID_METADATA, field: "ID3"},
		{name: "invalid artwork", data: mutateMP3Fixture(fixture, pngOffset, 0), code: library.INSPECTION_ERROR_INVALID_ARTWORK, field: "artwork"},
		{name: "invalid codec data", data: mutateMP3Fixture(fixture, audioOffset, 0), code: library.INSPECTION_ERROR_AUDIO_DECODE, field: "audio"},
		{name: "truncated middle", data: removeMP3FixtureBytes(fixture, audioOffset+(len(fixture)-audioOffset)/2, 8), code: library.INSPECTION_ERROR_AUDIO_DECODE, field: "audio"},
		{name: "truncated stream", data: fixture[:len(fixture)-8], code: library.INSPECTION_ERROR_AUDIO_DECODE, field: "audio"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := inspectMP3FixtureError(t, testCase.data)
			var inspectionErr *library.InspectionError
			if !errors.As(err, &inspectionErr) || inspectionErr.Code != testCase.code || inspectionErr.Field != testCase.field {
				t.Fatalf("inspection error = %T %+v", err, inspectionErr)
			}
		})
	}
}

func inspectMP3Fixture(t *testing.T, fixture []byte) library.MediaInspection {
	t.Helper()
	inspection, err := inspectMP3FixtureError(t, fixture)
	if err != nil {
		t.Fatalf("inspect MP3 fixture: %v", err)
	}
	return inspection
}

func inspectMP3FixtureError(t *testing.T, fixture []byte) (library.MediaInspection, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.mp3")
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatalf("write MP3 fixture: %v", err)
	}
	return library.NewMediaInspector().Inspect(context.Background(), path, nil)
}

func strictID3AudioOffset(t *testing.T, fixture []byte) int {
	t.Helper()
	if len(fixture) < 10 || string(fixture[:3]) != library.ID3_SIGNATURE {
		t.Fatal("strict MP3 fixture has no ID3 header")
	}
	return 10 + int(fixture[6])<<21 + int(fixture[7])<<14 + int(fixture[8])<<7 + int(fixture[9])
}

func mutateMP3Fixture(fixture []byte, offset int, value byte) []byte {
	mutated := append([]byte(nil), fixture...)
	mutated[offset] = value
	return mutated
}

func removeMP3FixtureBytes(fixture []byte, offset, count int) []byte {
	truncated := append([]byte(nil), fixture[:offset]...)
	return append(truncated, fixture[offset+count:]...)
}

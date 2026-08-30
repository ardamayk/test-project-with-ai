package library_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
)

func TestMediaInspectorInspectsStrictFLACFixture(t *testing.T) {
	inspector := library.NewMediaInspector()
	inspection, err := inspector.Inspect(filepath.Join("testdata", "strict-import.flac"))
	if err != nil {
		t.Fatalf("inspect fixture: %v", err)
	}
	assertNormalizedMetadata(t, inspection.Metadata)
	assertAlbumArtwork(t, inspection.AlbumArtwork)
	assertTechnicalAudio(t, inspection.Audio)
	if inspection.FileSHA256 != "7b64eef8dfac0c8b47b2601f9e7b6f748d8c110328c6d95a34c1d994988ac220" {
		t.Fatalf("file SHA-256 = %q", inspection.FileSHA256)
	}
}

func assertNormalizedMetadata(t *testing.T, metadata library.NormalizedMediaMetadata) {
	t.Helper()
	if metadata.Title != "Inspection Fixture" {
		t.Fatalf("title = %q", metadata.Title)
	}
	if !reflect.DeepEqual(metadata.Artists, []string{"Test Artist"}) {
		t.Fatalf("artists = %#v", metadata.Artists)
	}
	if !reflect.DeepEqual(metadata.AlbumArtists, []string{"Test Album Artist"}) {
		t.Fatalf("album artists = %#v", metadata.AlbumArtists)
	}
	if metadata.Album != "Strict Import Tests" {
		t.Fatalf("album = %q", metadata.Album)
	}
	if metadata.TrackPosition != (library.MediaPosition{Number: 3, Total: 9}) {
		t.Fatalf("track position = %+v", metadata.TrackPosition)
	}
	if metadata.DiscPosition != (library.MediaPosition{Number: 1, Total: 1}) {
		t.Fatalf("disc position = %+v", metadata.DiscPosition)
	}
	if !reflect.DeepEqual(metadata.Genres, []string{"Electronic"}) {
		t.Fatalf("genres = %#v", metadata.Genres)
	}
	if metadata.Year != 2026 {
		t.Fatalf("year = %d", metadata.Year)
	}
}

func assertAlbumArtwork(t *testing.T, artwork library.AlbumArtwork) {
	t.Helper()
	if artwork.MIMEType != "image/png" || artwork.Width != 32 || artwork.Height != 32 {
		t.Fatalf("artwork = %+v", artwork)
	}
	if len(artwork.Data) == 0 {
		t.Fatal("artwork data is empty")
	}
	if artwork.SHA256 != "d153c3ba2710fc0a3e364b3533812a538287d75bdfe407bd2f6e7c4c2358e85e" {
		t.Fatalf("artwork SHA-256 = %q", artwork.SHA256)
	}
}

func assertTechnicalAudio(t *testing.T, audio library.TechnicalAudioProperties) {
	t.Helper()
	if audio.Format != "flac" || audio.Codec != "flac" {
		t.Fatalf("format/codec = %s/%s", audio.Format, audio.Codec)
	}
	if audio.DurationMs != 250 || audio.SampleRateHz != 44100 {
		t.Fatalf("duration/sample rate = %d/%d", audio.DurationMs, audio.SampleRateHz)
	}
	if audio.ChannelCount != 1 || audio.BitDepth != 16 {
		t.Fatalf("channels/bit depth = %d/%d", audio.ChannelCount, audio.BitDepth)
	}
	if audio.BitrateKbps != 134 {
		t.Fatalf("bitrate = %d", audio.BitrateKbps)
	}
}

func TestMediaInspectorRejectsMissingRequiredMetadataWithStructuredCode(t *testing.T) {
	fixture := readInspectionFixture(t)
	fixture = bytes.Replace(fixture, []byte("TITLE=  Inspection   Fixture  "), []byte("XITLE=  Inspection   Fixture  "), 1)
	path := filepath.Join(t.TempDir(), "missing-title.flac")
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err := library.NewMediaInspector().Inspect(path)
	var inspectionErr *library.InspectionError
	if !errors.As(err, &inspectionErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if inspectionErr.Code != library.INSPECTION_ERROR_INVALID_METADATA || inspectionErr.Field != "TITLE" {
		t.Fatalf("inspection error = %+v", inspectionErr)
	}
}

func TestMediaInspectorRejectsTruncatedAudioWithStructuredCode(t *testing.T) {
	fixture := readInspectionFixture(t)
	path := filepath.Join(t.TempDir(), "truncated.flac")
	if err := os.WriteFile(path, fixture[:len(fixture)-8], 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err := library.NewMediaInspector().Inspect(path)
	var inspectionErr *library.InspectionError
	if !errors.As(err, &inspectionErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if inspectionErr.Code != library.INSPECTION_ERROR_AUDIO_DECODE {
		t.Fatalf("inspection error = %+v", inspectionErr)
	}
}

func readInspectionFixture(t *testing.T) []byte {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("testdata", "strict-import.flac"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return fixture
}

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

	if inspection.Metadata.Title != "Inspection Fixture" {
		t.Fatalf("title = %q", inspection.Metadata.Title)
	}
	if !reflect.DeepEqual(inspection.Metadata.Artists, []string{"Test Artist"}) {
		t.Fatalf("artists = %#v", inspection.Metadata.Artists)
	}
	if !reflect.DeepEqual(inspection.Metadata.AlbumArtists, []string{"Test Album Artist"}) {
		t.Fatalf("album artists = %#v", inspection.Metadata.AlbumArtists)
	}
	if inspection.Metadata.Album != "Strict Import Tests" {
		t.Fatalf("album = %q", inspection.Metadata.Album)
	}
	if inspection.Metadata.TrackNumber != 3 || inspection.Metadata.TrackTotal != 9 {
		t.Fatalf("track position = %d/%d", inspection.Metadata.TrackNumber, inspection.Metadata.TrackTotal)
	}
	if inspection.Metadata.DiscNumber != 1 || inspection.Metadata.DiscTotal != 1 {
		t.Fatalf("disc position = %d/%d", inspection.Metadata.DiscNumber, inspection.Metadata.DiscTotal)
	}
	if !reflect.DeepEqual(inspection.Metadata.Genres, []string{"Electronic"}) {
		t.Fatalf("genres = %#v", inspection.Metadata.Genres)
	}
	if inspection.Metadata.Year != 2026 {
		t.Fatalf("year = %d", inspection.Metadata.Year)
	}

	if inspection.Artwork.MIMEType != "image/png" || inspection.Artwork.Width != 32 || inspection.Artwork.Height != 32 {
		t.Fatalf("artwork = %+v", inspection.Artwork)
	}
	if len(inspection.Artwork.Data) == 0 {
		t.Fatal("artwork data is empty")
	}
	if inspection.Artwork.SHA256 != "d153c3ba2710fc0a3e364b3533812a538287d75bdfe407bd2f6e7c4c2358e85e" {
		t.Fatalf("artwork SHA-256 = %q", inspection.Artwork.SHA256)
	}

	if inspection.Audio.Format != "flac" || inspection.Audio.Codec != "flac" {
		t.Fatalf("format/codec = %s/%s", inspection.Audio.Format, inspection.Audio.Codec)
	}
	if inspection.Audio.DurationMs != 250 || inspection.Audio.SampleRateHz != 44100 {
		t.Fatalf("duration/sample rate = %d/%d", inspection.Audio.DurationMs, inspection.Audio.SampleRateHz)
	}
	if inspection.Audio.ChannelCount != 1 || inspection.Audio.BitDepth != 16 {
		t.Fatalf("channels/bit depth = %d/%d", inspection.Audio.ChannelCount, inspection.Audio.BitDepth)
	}
	if inspection.Audio.BitrateKbps != 134 {
		t.Fatalf("bitrate = %d", inspection.Audio.BitrateKbps)
	}
	if inspection.FileSHA256 != "7b64eef8dfac0c8b47b2601f9e7b6f748d8c110328c6d95a34c1d994988ac220" {
		t.Fatalf("file SHA-256 = %q", inspection.FileSHA256)
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

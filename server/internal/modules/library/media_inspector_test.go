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
)

func TestMediaInspectorInspectsStrictFLACFixture(t *testing.T) {
	inspector := library.NewMediaInspector()
	var progress []library.InspectionProgress
	inspection, err := inspector.Inspect(context.Background(), filepath.Join("testdata", "strict-import.flac"), func(update library.InspectionProgress) error {
		progress = append(progress, update)
		return nil
	})
	if err != nil {
		t.Fatalf("inspect fixture: %v", err)
	}
	assertNormalizedMetadata(t, inspection.Metadata)
	assertAlbumArtwork(t, inspection.AlbumArtwork)
	assertTechnicalAudio(t, inspection.Audio)
	if inspection.FileSHA256 != "7b64eef8dfac0c8b47b2601f9e7b6f748d8c110328c6d95a34c1d994988ac220" {
		t.Fatalf("file SHA-256 = %q", inspection.FileSHA256)
	}
	assertCompletedInspectionProgress(t, progress)
}

func TestMediaInspectorDetectsFLACIndependentlyOfExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "misleading.mp3")
	if err := os.WriteFile(path, readInspectionFixture(t), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	inspection, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("inspect renamed fixture: %v", err)
	}
	if inspection.Audio.Container != "flac" || inspection.Audio.Codec != "flac" {
		t.Fatalf("detected container/codec = %s/%s", inspection.Audio.Container, inspection.Audio.Codec)
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
	if audio.Format != "flac" || audio.Container != "flac" || audio.Codec != "flac" {
		t.Fatalf("format/container/codec = %s/%s/%s", audio.Format, audio.Container, audio.Codec)
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

	_, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
	var inspectionErr *library.InspectionError
	if !errors.As(err, &inspectionErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if inspectionErr.Code != library.INSPECTION_ERROR_INVALID_METADATA || inspectionErr.Field != "TITLE" {
		t.Fatalf("inspection error = %+v", inspectionErr)
	}
}

func TestMediaInspectorRejectsBeginningMiddleAndEndTruncation(t *testing.T) {
	fixture := readInspectionFixture(t)
	tests := []struct {
		name          string
		fixture       []byte
		expectedCode  library.InspectionErrorCode
		expectedField string
	}{
		{name: "beginning", fixture: fixture[4:], expectedCode: library.INSPECTION_ERROR_UNSUPPORTED_FORMAT, expectedField: "container"},
		{name: "middle", fixture: truncateMiddleAudio(t, fixture), expectedCode: library.INSPECTION_ERROR_AUDIO_DECODE, expectedField: "audio"},
		{name: "end", fixture: fixture[:len(fixture)-8], expectedCode: library.INSPECTION_ERROR_AUDIO_DECODE, expectedField: "audio"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), test.name+"-truncated.flac")
			if err := os.WriteFile(path, test.fixture, 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			_, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
			var inspectionErr *library.InspectionError
			if !errors.As(err, &inspectionErr) {
				t.Fatalf("error = %T %v", err, err)
			}
			if inspectionErr.Code != test.expectedCode || inspectionErr.Field != test.expectedField {
				t.Fatalf("inspection error = %+v", inspectionErr)
			}
		})
	}
}

func TestMediaInspectorReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var lastProgress library.InspectionProgress

	_, err := library.NewMediaInspector().Inspect(ctx, filepath.Join("testdata", "strict-import.flac"), func(update library.InspectionProgress) error {
		lastProgress = update
		if update.Percent > 0 {
			cancel()
		}
		return nil
	})

	var inspectionErr *library.InspectionError
	if !errors.As(err, &inspectionErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if inspectionErr.Code != library.INSPECTION_ERROR_VALIDATION_CANCELLED || inspectionErr.Field != "validation" {
		t.Fatalf("inspection error = %+v", inspectionErr)
	}
	if lastProgress.Percent <= 0 || lastProgress.Percent >= 100 {
		t.Fatalf("progress before cancellation = %+v", lastProgress)
	}
}

func TestMediaInspectorReportsCancellationFromProgressObserver(t *testing.T) {
	_, err := library.NewMediaInspector().Inspect(context.Background(), filepath.Join("testdata", "strict-import.flac"), func(library.InspectionProgress) error {
		return context.Canceled
	})

	var inspectionErr *library.InspectionError
	if !errors.As(err, &inspectionErr) || inspectionErr.Code != library.INSPECTION_ERROR_VALIDATION_CANCELLED {
		t.Fatalf("inspection error = %T %v", err, err)
	}
}

func TestMediaInspectorReportsProgressWhenStreamSampleTotalIsUnknown(t *testing.T) {
	fixture := readInspectionFixture(t)
	fixture[21] &= 0xF0
	clear(fixture[22:26])
	path := filepath.Join(t.TempDir(), "unknown-total.flac")
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	var progress []library.InspectionProgress

	_, err := library.NewMediaInspector().Inspect(context.Background(), path, func(update library.InspectionProgress) error {
		progress = append(progress, update)
		return nil
	})

	if err != nil {
		t.Fatalf("inspect unknown-total fixture: %v", err)
	}
	if len(progress) < 2 || progress[0].Percent <= 0 || progress[0].Percent >= 100 {
		t.Fatalf("unknown-total inspection progress = %+v", progress)
	}
	if progress[len(progress)-1].Percent != 100 {
		t.Fatalf("completed unknown-total progress = %+v", progress[len(progress)-1])
	}
}

func assertCompletedInspectionProgress(t *testing.T, progress []library.InspectionProgress) {
	t.Helper()
	if len(progress) == 0 {
		t.Fatal("inspection progress is empty")
	}
	previousPercent := 0
	for _, update := range progress {
		if update.Percent < previousPercent || update.Percent < 0 || update.Percent > 100 {
			t.Fatalf("inspection progress is invalid: %+v", progress)
		}
		previousPercent = update.Percent
	}
	last := progress[len(progress)-1]
	if last.Percent != 100 || last.DecodedSamples == 0 || last.DecodedSamples != last.TotalSamples {
		t.Fatalf("completed inspection progress = %+v", last)
	}
}

func truncateMiddleAudio(t *testing.T, fixture []byte) []byte {
	t.Helper()
	audioOffset := flacAudioOffset(t, fixture)
	middle := audioOffset + (len(fixture)-audioOffset)/2
	truncated := append([]byte(nil), fixture[:middle]...)
	return append(truncated, fixture[middle+8:]...)
}

func flacAudioOffset(t *testing.T, fixture []byte) int {
	t.Helper()
	offset := 4
	for {
		if offset+4 > len(fixture) {
			t.Fatal("FLAC fixture metadata header is truncated")
		}
		isLast := fixture[offset]&0x80 != 0
		length := int(fixture[offset+1])<<16 | int(fixture[offset+2])<<8 | int(fixture[offset+3])
		offset += 4 + length
		if isLast {
			return offset
		}
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

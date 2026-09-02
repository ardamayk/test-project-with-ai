package library_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

type wavFixture struct {
	format      wavFormatSpec
	pcmFrames   int
	id3Frames   []id3Frame
	omitID3     bool
	riffSizeFix func(riffSize uint32) uint32
}

type wavFormatSpec struct {
	AudioFormat  uint16
	ChannelCount uint16
	SampleRateHz uint32
	BitDepth     uint16
	ValidBits    uint16
}

type id3Frame struct {
	id   string
	body []byte
}

const (
	WAV_TEST_SAMPLE_RATE_HZ = 44100
	WAV_TEST_CHANNELS       = 2
	WAV_TEST_BIT_DEPTH      = 16
	WAV_TEST_PCM_FRAMES     = 4410
)

func strictWAVFixture() wavFixture {
	return wavFixture{
		format: wavFormatSpec{
			AudioFormat:  1,
			ChannelCount: WAV_TEST_CHANNELS,
			SampleRateHz: WAV_TEST_SAMPLE_RATE_HZ,
			BitDepth:     WAV_TEST_BIT_DEPTH,
		},
		pcmFrames: WAV_TEST_PCM_FRAMES,
		id3Frames: []id3Frame{
			textID3Frame("TIT2", "Inspection Fixture"),
			textID3Frame("TPE1", "Test Artist"),
			textID3Frame("TPE2", "Test Album Artist"),
			textID3Frame("TALB", "Strict Import Tests"),
			textID3Frame("TRCK", "3/9"),
			textID3Frame("TPOS", "1/1"),
			textID3Frame("TCON", "Electronic"),
			textID3Frame("TDRC", "2026"),
			apicID3Frame(3, "image/png", "cover", encodeTestPNG()),
		},
	}
}

func textID3Frame(id, value string) id3Frame {
	body := append([]byte{0x03}, []byte(value)...)
	return id3Frame{id: id, body: body}
}

func apicID3Frame(pictureType int, mimeType, description string, data []byte) id3Frame {
	var body []byte
	body = append(body, 0x00)
	body = append(body, []byte(mimeType)...)
	body = append(body, 0x00)
	body = append(body, byte(pictureType))
	body = append(body, []byte(description)...)
	body = append(body, 0x00)
	body = append(body, data...)
	return id3Frame{id: "APIC", body: body}
}

func encodeTestPNG() []byte {
	return testutil.WAVCoverPNG()
}

func encodeWAV(t *testing.T, fixture wavFixture) []byte {
	t.Helper()
	frames := make([]testutil.WAVID3Frame, len(fixture.id3Frames))
	for index, frame := range fixture.id3Frames {
		frames[index] = testutil.WAVID3Frame{ID: frame.id, Body: frame.body}
	}
	return testutil.EncodeWAV(t, testutil.WAVFixture{
		AudioFormat:  fixture.format.AudioFormat,
		ChannelCount: fixture.format.ChannelCount,
		SampleRateHz: fixture.format.SampleRateHz,
		BitDepth:     fixture.format.BitDepth,
		ValidBits:    fixture.format.ValidBits,
		PCMFrames:    fixture.pcmFrames,
		ID3Frames:    frames,
		OmitID3:      fixture.omitID3,
		RIFFSizeFix:  fixture.riffSizeFix,
	})
}

func writeWAVFixture(t *testing.T, fixture wavFixture) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "strict-import.wav")
	if err := os.WriteFile(path, encodeWAV(t, fixture), 0o600); err != nil {
		t.Fatalf("write WAV fixture: %v", err)
	}
	return path
}

func TestMediaInspectorInspectsStrictWAVFixture(t *testing.T) {
	var progress []library.InspectionProgress
	inspection, err := library.NewMediaInspector().Inspect(context.Background(), writeWAVFixture(t, strictWAVFixture()), func(update library.InspectionProgress) error {
		progress = append(progress, update)
		return nil
	})
	if err != nil {
		t.Fatalf("inspect WAV fixture: %v", err)
	}
	if inspection.Metadata.Title != "Inspection Fixture" || inspection.Metadata.Album != "Strict Import Tests" {
		t.Fatalf("metadata = %+v", inspection.Metadata)
	}
	if inspection.Metadata.TrackPosition.Number != 3 || inspection.Metadata.TrackPosition.Total != 9 {
		t.Fatalf("track position = %+v", inspection.Metadata.TrackPosition)
	}
	if inspection.AlbumArtwork.MIMEType != "image/png" || inspection.AlbumArtwork.Width != 2 {
		t.Fatalf("artwork = %+v", inspection.AlbumArtwork)
	}
	if inspection.Audio.Format != "wav" || inspection.Audio.Container != "wav" || inspection.Audio.Codec != "pcm_s16le" {
		t.Fatalf("audio = %+v", inspection.Audio)
	}
	if inspection.Audio.DurationMs != 100 || inspection.Audio.SampleRateHz != WAV_TEST_SAMPLE_RATE_HZ || inspection.Audio.ChannelCount != WAV_TEST_CHANNELS || inspection.Audio.BitDepth != WAV_TEST_BIT_DEPTH || inspection.Audio.BitrateKbps != 1411 {
		t.Fatalf("audio technical properties = %+v", inspection.Audio)
	}
	assertCompletedInspectionProgress(t, progress)
}

func TestMediaInspectorDetectsWAVIndependentlyOfExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "misleading.mp3")
	if err := os.WriteFile(path, encodeWAV(t, strictWAVFixture()), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	inspection, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("inspect renamed fixture: %v", err)
	}
	if inspection.Audio.Container != "wav" || inspection.Audio.Codec != "pcm_s16le" {
		t.Fatalf("detected container/codec = %s/%s", inspection.Audio.Container, inspection.Audio.Codec)
	}
}

func TestMediaInspectorInspectsExtensiblePCMWAV(t *testing.T) {
	fixture := strictWAVFixture()
	fixture.format.AudioFormat = 0xfffe
	fixture.format.BitDepth = 24

	inspection, err := library.NewMediaInspector().Inspect(context.Background(), writeWAVFixture(t, fixture), nil)
	if err != nil {
		t.Fatalf("inspect extensible WAV fixture: %v", err)
	}
	if inspection.Audio.Codec != "pcm_s24le" || inspection.Audio.BitDepth != 24 {
		t.Fatalf("extensible WAV audio = %+v", inspection.Audio)
	}
}

func TestMediaInspectorReportsValidBitsForExtensiblePCMWAV(t *testing.T) {
	fixture := strictWAVFixture()
	fixture.format.AudioFormat = 0xfffe
	fixture.format.BitDepth = 32
	fixture.format.ValidBits = 24

	inspection, err := library.NewMediaInspector().Inspect(context.Background(), writeWAVFixture(t, fixture), nil)
	if err != nil {
		t.Fatalf("inspect extensible WAV with reduced valid bits: %v", err)
	}
	if inspection.Audio.Codec != "pcm_s32le" || inspection.Audio.BitDepth != 24 {
		t.Fatalf("extensible WAV audio = %+v", inspection.Audio)
	}
}

func TestMediaInspectorRejectsValidBitsWiderThanExtensiblePCMContainer(t *testing.T) {
	fixture := strictWAVFixture()
	fixture.format.AudioFormat = 0xfffe
	fixture.format.BitDepth = 32
	fixture.format.ValidBits = 40

	_, err := library.NewMediaInspector().Inspect(context.Background(), writeWAVFixture(t, fixture), nil)
	assertInspectionError(t, err, library.INSPECTION_ERROR_AUDIO_DECODE, "audio")
}

func TestMediaInspectorPersistsUnsignedEightBitPCMFormat(t *testing.T) {
	fixture := strictWAVFixture()
	fixture.format.ChannelCount = 1
	fixture.format.BitDepth = 8

	inspection, err := library.NewMediaInspector().Inspect(context.Background(), writeWAVFixture(t, fixture), nil)
	if err != nil {
		t.Fatalf("inspect eight-bit WAV fixture: %v", err)
	}
	if inspection.Audio.Codec != "pcm_u8" || inspection.Audio.BitDepth != 8 {
		t.Fatalf("eight-bit WAV audio = %+v", inspection.Audio)
	}
}

func TestMediaInspectorParsesID3TextEncodingsAndMultipleValues(t *testing.T) {
	fixture := strictWAVFixture()
	for index, frame := range fixture.id3Frames {
		switch frame.id {
		case "TPE1":
			fixture.id3Frames[index].body = append([]byte{0x03}, []byte("Artist One\x00Artist Two")...)
		case "TPE2":
			fixture.id3Frames[index].body = append([]byte{0x00}, []byte{'A', 'n', 'd', 'r', 0xe9}...)
		}
	}

	inspection, err := library.NewMediaInspector().Inspect(context.Background(), writeWAVFixture(t, fixture), nil)
	if err != nil {
		t.Fatalf("inspect encoded ID3 values: %v", err)
	}
	if len(inspection.Metadata.Artists) != 2 || inspection.Metadata.Artists[0] != "Artist One" || inspection.Metadata.Artists[1] != "Artist Two" {
		t.Fatalf("ID3 artists = %+v", inspection.Metadata.Artists)
	}
	if len(inspection.Metadata.AlbumArtists) != 1 || inspection.Metadata.AlbumArtists[0] != "André" {
		t.Fatalf("ID3 album artists = %+v", inspection.Metadata.AlbumArtists)
	}
}

func TestMediaInspectorRejectsUntaggedWAVByDesign(t *testing.T) {
	fixture := strictWAVFixture()
	fixture.id3Frames = nil
	fixture.omitID3 = true
	_, err := library.NewMediaInspector().Inspect(context.Background(), writeWAVFixture(t, fixture), nil)
	assertInspectionError(t, err, library.INSPECTION_ERROR_INVALID_METADATA, "metadata")
}

func TestMediaInspectorRejectsWAVWithoutFrontCoverArtwork(t *testing.T) {
	fixture := strictWAVFixture()
	frames := make([]id3Frame, 0, len(fixture.id3Frames))
	for _, frame := range fixture.id3Frames {
		if frame.id != "APIC" {
			frames = append(frames, frame)
		}
	}
	fixture.id3Frames = frames
	_, err := library.NewMediaInspector().Inspect(context.Background(), writeWAVFixture(t, fixture), nil)
	assertInspectionError(t, err, library.INSPECTION_ERROR_MISSING_ARTWORK, "artwork")
}

func TestMediaInspectorRejectsWAVWithMissingIdentityTag(t *testing.T) {
	for _, dropped := range []string{"TIT2", "TPE1", "TPE2", "TALB", "TRCK", "TCON"} {
		t.Run(dropped, func(t *testing.T) {
			fixture := strictWAVFixture()
			frames := make([]id3Frame, 0, len(fixture.id3Frames))
			for _, frame := range fixture.id3Frames {
				if frame.id != dropped {
					frames = append(frames, frame)
				}
			}
			fixture.id3Frames = frames
			_, err := library.NewMediaInspector().Inspect(context.Background(), writeWAVFixture(t, fixture), nil)
			var inspectionErr *library.InspectionError
			if !errors.As(err, &inspectionErr) || inspectionErr.Code != library.INSPECTION_ERROR_INVALID_METADATA {
				t.Fatalf("error = %T %v", err, err)
			}
		})
	}
}

func TestMediaInspectorRejectsBrokenWAVContainers(t *testing.T) {
	forged := func(riffSize uint32) uint32 { return riffSize + 64 }
	truncated := encodeWAV(t, strictWAVFixture())
	truncated = truncated[:len(truncated)-40]
	tests := []struct {
		name         string
		fixture      wavFixture
		raw          []byte
		expectedCode library.InspectionErrorCode
	}{
		{name: "inflated RIFF size", fixture: wavFixture{format: strictWAVFixture().format, pcmFrames: WAV_TEST_PCM_FRAMES, id3Frames: strictWAVFixture().id3Frames, riffSizeFix: forged}, expectedCode: library.INSPECTION_ERROR_UNSUPPORTED_FORMAT},
		{name: "truncated data chunk", raw: truncated, expectedCode: library.INSPECTION_ERROR_UNSUPPORTED_FORMAT},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "broken.wav")
			data := test.raw
			if data == nil {
				data = encodeWAV(t, test.fixture)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			_, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
			var inspectionErr *library.InspectionError
			if !errors.As(err, &inspectionErr) {
				t.Fatalf("error = %T %v", err, err)
			}
			if inspectionErr.Code != test.expectedCode {
				t.Fatalf("inspection error = %+v", inspectionErr)
			}
		})
	}
}

func TestMediaInspectorRejectsMissingRIFFPadding(t *testing.T) {
	fixture := strictWAVFixture()
	fixture.format.ChannelCount = 1
	fixture.format.BitDepth = 8
	fixture.pcmFrames = WAV_TEST_PCM_FRAMES + 1
	data := encodeWAV(t, fixture)
	data = data[:len(data)-1]
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))

	path := filepath.Join(t.TempDir(), "missing-padding.wav")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
	assertInspectionError(t, err, library.INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container")
}

func TestMediaInspectorRejectsMalformedID3SyncsafeSize(t *testing.T) {
	data := encodeWAV(t, strictWAVFixture())
	id3Offset := bytes.Index(data, []byte("ID3 "))
	if id3Offset < 0 {
		t.Fatal("ID3 chunk is missing from fixture")
	}
	data[id3Offset+8+6] |= 0x80

	path := filepath.Join(t.TempDir(), "malformed-id3.wav")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
	assertInspectionError(t, err, library.INSPECTION_ERROR_INVALID_METADATA, "ID3")
}

func TestMediaInspectorRejectsEmptyID3Frame(t *testing.T) {
	fixture := strictWAVFixture()
	fixture.id3Frames = append([]id3Frame{{id: "PRIV"}}, fixture.id3Frames...)

	_, err := library.NewMediaInspector().Inspect(context.Background(), writeWAVFixture(t, fixture), nil)
	assertInspectionError(t, err, library.INSPECTION_ERROR_INVALID_METADATA, "ID3")
}

func TestMediaInspectorRejectsNonPCMWAV(t *testing.T) {
	fixture := strictWAVFixture()
	fixture.format.AudioFormat = 3 // IEEE float
	_, err := library.NewMediaInspector().Inspect(context.Background(), writeWAVFixture(t, fixture), nil)
	assertInspectionError(t, err, library.INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container")
}

func TestMediaInspectorRejectsUnalignedWAVDataChunk(t *testing.T) {
	fixture := strictWAVFixture()
	fixture.pcmFrames = 1
	// blockAlign = 4 bytes; craft 5 bytes by appending one stray byte through
	// an odd data chunk: emulate by bit depth 16, channels 2, frames 1 + extra.
	_ = fixture
	data := encodeWAV(t, fixture)
	// Locate data chunk and add one extra byte to its body.
	index := bytes.LastIndex(data, []byte("data"))
	chunkSize := binary.LittleEndian.Uint32(data[index+4 : index+8])
	binary.LittleEndian.PutUint32(data[index+4:index+8], chunkSize+1)
	riffSize := binary.LittleEndian.Uint32(data[4:8])
	binary.LittleEndian.PutUint32(data[4:8], riffSize+2)
	data = append(data, 0x00, 0x00)
	path := filepath.Join(t.TempDir(), "unaligned.wav")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
	assertInspectionError(t, err, library.INSPECTION_ERROR_AUDIO_DECODE, "audio")
}

func assertInspectionError(t *testing.T, err error, expectedCode library.InspectionErrorCode, expectedField string) {
	t.Helper()
	var inspectionErr *library.InspectionError
	if !errors.As(err, &inspectionErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if inspectionErr.Code != expectedCode || inspectionErr.Field != expectedField {
		t.Fatalf("inspection error = %+v", inspectionErr)
	}
}

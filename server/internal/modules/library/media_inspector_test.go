package library_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	flacmeta "github.com/mewkiz/flac/meta"
)

const (
	FLAC_METADATA_BLOCK_LAST_FLAG           byte   = 0x80
	FLAC_METADATA_BLOCK_TYPE_MASK           byte   = 0x7f
	TEST_ARTWORK_WIDTH                             = 2
	TEST_ARTWORK_HEIGHT                            = 2
	TEST_ARTWORK_DEPTH                             = 24
	PNG_CHUNK_LENGTH_SIZE_BYTES                    = 4
	PNG_IHDR_WIDTH_OFFSET                          = 16
	PNG_IHDR_HEIGHT_OFFSET                         = 20
	PNG_IHDR_CRC_OFFSET                            = 29
	PNG_IHDR_CRC_INPUT_OFFSET                      = 12
	PNG_ANIMATION_CONTROL_FIELD_COUNT              = 2
	ANIMATED_WEBP_FIXTURE_BASE64                   = "UklGRhYAAABXRUJQVlA4WAoAAAACAAAAAAAAAAAA"
	FLAC_METADATA_LENGTH_HIGH_BYTE_OFFSET          = 1
	FLAC_METADATA_LENGTH_MIDDLE_BYTE_OFFSET        = 2
	FLAC_METADATA_LENGTH_LOW_BYTE_OFFSET           = 3
	FLAC_METADATA_LENGTH_HIGH_SHIFT                = 16
	FLAC_METADATA_LENGTH_MIDDLE_SHIFT              = 8
	TRUNCATED_PNG_SUFFIX_BYTES                     = 8
	PNG_ANIMATION_FRAME_COUNT                      = 1
	OVERSIZED_ARTWORK_WIDTH                        = 10_000
	OVERSIZED_ARTWORK_HEIGHT                       = 5_001
	OGG_CHECKSUM_OFFSET                            = 22
	OGG_CHECKSUM_SIZE_BYTES                        = 4
	OGG_PAGE_SEGMENT_COUNT_OFFSET                  = 26
	OGG_CHECKSUM_POLYNOMIAL                 uint32 = 0x04c11db7
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

func TestMediaInspectorPrefersFLACPluralCredits(t *testing.T) {
	path := writeFLACWithExtraVorbisTags(t, [][2]string{
		{"ARTISTS", "Earth, Wind & Fire"}, {"artists", "AC/DC; Live"},
		{"ALBUMARTISTS", "Album / One"}, {"albumartists", "Album & Two"},
	})
	inspection, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inspection.Metadata.Artists, []string{"Earth, Wind & Fire", "AC/DC; Live"}) ||
		!reflect.DeepEqual(inspection.Metadata.AlbumArtists, []string{"Album / One", "Album & Two"}) {
		t.Fatalf("plural credits = %+v", inspection.Metadata)
	}
}

func TestMediaInspectorArtistCreditPrecedence(t *testing.T) {
	for _, format := range []string{"flac", "ogg", "opus", "mp3v2", "mp3v3", "mp3v4", "wav", "m4a"} {
		for _, test := range []struct {
			name         string
			credits      [][2]string
			artists      []string
			albumArtists []string
			invalidField string
		}{
			{name: "singular punctuation", credits: [][2]string{{"ARTIST", "Earth, Wind & Fire / Live; Guests"}, {"ALBUMARTIST", "AC/DC"}}, artists: []string{"Earth, Wind & Fire / Live; Guests"}, albumArtists: []string{"AC/DC"}},
			{name: "plural track only", credits: [][2]string{{"ARTIST", "Display"}, {"ALBUMARTIST", "Album"}, {"ARTISTS", "Second"}, {"ARTISTS", "First"}}, artists: []string{"Second", "First"}, albumArtists: []string{"Album"}},
			{name: "plural album only", credits: [][2]string{{"ARTIST", "Track"}, {"ALBUMARTIST", "Display"}, {"ALBUMARTISTS", "Second"}, {"ALBUMARTISTS", "First"}}, artists: []string{"Track"}, albumArtists: []string{"Second", "First"}},
			{name: "plural without singular", credits: [][2]string{{"ARTISTS", "Earth, Wind & Fire"}, {"ARTISTS", "AC/DC; Live"}, {"ALBUMARTISTS", "Album / One"}, {"ALBUMARTISTS", "Album & Two"}}, artists: []string{"Earth, Wind & Fire", "AC/DC; Live"}, albumArtists: []string{"Album / One", "Album & Two"}},
			{name: "empty plural track", credits: [][2]string{{"ARTIST", "Track"}, {"ALBUMARTIST", "Album"}, {"ARTISTS", ""}}, invalidField: "ARTISTS"},
			{name: "empty plural album", credits: [][2]string{{"ARTIST", "Track"}, {"ALBUMARTIST", "Album"}, {"ALBUMARTISTS", ""}}, invalidField: "ALBUMARTISTS"},
			{name: "invalid later plural track", credits: [][2]string{{"ARTIST", "Track"}, {"ALBUMARTIST", "Album"}, {"ARTISTS", "Valid"}, {"ARTISTS", "  "}}, invalidField: "ARTISTS"},
			{name: "unsafe plural album", credits: [][2]string{{"ARTIST", "Track"}, {"ALBUMARTIST", "Album"}, {"ALBUMARTISTS", "Bad\u202e"}}, invalidField: "ALBUMARTISTS"},
			{name: "oversized plural", credits: [][2]string{{"ARTIST", "Track"}, {"ALBUMARTIST", "Album"}, {"ARTISTS", strings.Repeat("x", 201)}}, invalidField: "ARTISTS"},
			{name: "album never inferred", credits: [][2]string{{"ARTISTS", "Track"}}, invalidField: "ALBUMARTIST"},
			{name: "track never inferred", credits: [][2]string{{"ALBUMARTISTS", "Album"}}, invalidField: "ARTIST"},
		} {
			t.Run(format+"/"+test.name, func(t *testing.T) {
				path := writeArtistCreditFixture(t, format, test.credits)
				inspection, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
				if test.invalidField != "" {
					field := test.invalidField
					if format == "m4a" && test.name == "oversized plural" {
						field = "credits" // MP4 rejects oversized atoms before normalization.
					}
					assertInspectionError(t, err, library.INSPECTION_ERROR_INVALID_METADATA, field)
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(inspection.Metadata.Artists, test.artists) || !reflect.DeepEqual(inspection.Metadata.AlbumArtists, test.albumArtists) {
					t.Fatalf("credits = %q / %q; want %q / %q", inspection.Metadata.Artists, inspection.Metadata.AlbumArtists, test.artists, test.albumArtists)
				}
			})
		}
	}
}

func TestMediaInspectorRejectsM4APluralAtomWithoutValues(t *testing.T) {
	for _, key := range []string{"ARTISTS", "ALBUMARTISTS"} {
		t.Run(key, func(t *testing.T) {
			fixture := m4aArtistCreditFixture(t, [][2]string{{"ARTIST", "Track"}, {"ALBUMARTIST", "Album"}, {key, "Removed credit"}})
			valueOffset := bytes.Index(fixture, []byte("Removed credit"))
			if valueOffset < 12 || string(fixture[valueOffset-12:valueOffset-8]) != "data" {
				t.Fatal("credit data atom missing")
			}
			copy(fixture[valueOffset-12:valueOffset-8], "free")
			path := filepath.Join(t.TempDir(), "empty-credits.m4a")
			if err := os.WriteFile(path, fixture, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
			assertInspectionError(t, err, library.INSPECTION_ERROR_INVALID_METADATA, key)
		})
	}
}

func writeArtistCreditFixture(t *testing.T, format string, credits [][2]string) string {
	t.Helper()
	var fixture []byte
	switch format {
	case "flac", "ogg", "opus":
		tags := append([][2]string{{"TITLE", "Credits"}, {"ALBUM", "Credits Album"}, {"TRACKNUMBER", "1"}}, credits...)
		comments := appendVorbisComments(t, make([]byte, 8), tags)
		if format == "flac" {
			blocks, audio := splitFLACMetadata(t, readInspectionFixture(t))
			for i := range blocks {
				if blocks[i].blockType == byte(flacmeta.TypeVorbisComment) {
					blocks[i].body = comments
				}
			}
			fixture = encodeFLACFixture(t, blocks, audio)
		} else {
			fixture = replaceOGGCommentPacket(t, readOGGFixture(t, "strict-import."+format), comments)
		}
	case "wav":
		wav := strictWAVFixture()
		wav.id3Frames = append(wav.id3Frames[:1], wav.id3Frames[3:]...)
		for _, credit := range credits {
			id, value := "TXXX", credit[0]+"\x00"+credit[1]
			switch credit[0] {
			case "ARTIST":
				id, value = "TPE1", credit[1]
			case "ALBUMARTIST":
				id, value = "TPE2", credit[1]
			}
			wav.id3Frames = append(wav.id3Frames, textID3Frame(id, value))
		}
		fixture = encodeWAV(t, wav)
	case "m4a":
		fixture = m4aArtistCreditFixture(t, credits)
	default:
		version := format[len(format)-1] - '0'
		artistID, albumID, customID := "TPE1", "TPE2", "TXXX"
		if version == 2 {
			artistID, albumID, customID = "TP1", "TP2", "TXX"
		}
		fixture = testutil.StrictMP3FixtureWithID3Version(version)
		fixture = bytes.ReplaceAll(fixture, []byte(artistID), bytes.Repeat([]byte("X"), len(artistID)))
		fixture = bytes.ReplaceAll(fixture, []byte(albumID), bytes.Repeat([]byte("X"), len(albumID)))
		var frames []byte
		for _, credit := range credits {
			id, value := customID, credit[0]+"\x00"+credit[1]
			switch credit[0] {
			case "ARTIST":
				id, value = artistID, credit[1]
			case "ALBUMARTIST":
				id, value = albumID, credit[1]
			}
			frame := testutil.ID3TextFrame(version, id, value)
			// Test UTF-8 payloads in every tag version; encoding acceptance is unchanged.
			frame[len(frame)-len(value)-1] = 3
			frames = append(frames, frame...)
		}
		offset := strictID3AudioOffset(t, fixture)
		fixture = append(append(append([]byte(nil), fixture[:offset]...), frames...), fixture[offset:]...)
		size := offset - 10 + len(frames)
		copy(fixture[6:10], []byte{byte(size >> 21), byte(size >> 14 & 0x7f), byte(size >> 7 & 0x7f), byte(size & 0x7f)})
	}
	path := filepath.Join(t.TempDir(), "credits."+format)
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func m4aArtistCreditFixture(t *testing.T, credits [][2]string) []byte {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("testdata", "strict-import-aac.m4a"))
	if err != nil {
		t.Fatal(err)
	}
	atom := func(name string, body []byte) []byte {
		data := binary.BigEndian.AppendUint32(nil, uint32(len(body)+8))
		return append(append(data, name...), body...)
	}
	var creditAtoms []byte
	for _, credit := range credits {
		data := atom("data", append([]byte{0, 0, 0, 1, 0, 0, 0, 0}, credit[1]...))
		name := "----"
		switch credit[0] {
		case "ARTIST":
			name = "\xa9ART"
		case "ALBUMARTIST":
			name = "aART"
		default:
			body := atom("mean", append(make([]byte, 4), "com.apple.iTunes"...))
			body = append(body, atom("name", append(make([]byte, 4), credit[0]...))...)
			data = append(body, data...)
		}
		creditAtoms = append(creditAtoms, atom(name, data)...)
	}
	var rewrite func([]byte) []byte
	rewrite = func(input []byte) []byte {
		var output []byte
		for offset := 0; offset < len(input); {
			size := int(binary.BigEndian.Uint32(input[offset:]))
			name := string(input[offset+4 : offset+8])
			body := input[offset+8 : offset+size]
			switch name {
			case "moov", "udta":
				body = rewrite(body)
			case "meta":
				body = append(append([]byte(nil), body[:4]...), rewrite(body[4:])...)
			case "ilst":
				body = append(rewrite(body), creditAtoms...)
			case "\xa9ART", "aART":
				offset += size
				continue
			}
			output = append(output, atom(name, body)...)
			offset += size
		}
		return output
	}
	// Keep all media offsets unchanged: move moov after mdat, leave free space.
	var output, metadata []byte
	for offset := 0; offset < len(fixture); {
		size := int(binary.BigEndian.Uint32(fixture[offset:]))
		if string(fixture[offset+4:offset+8]) == "moov" {
			metadata = rewrite(fixture[offset : offset+size])
			output = append(output, atom("free", make([]byte, size-8))...)
		} else {
			output = append(output, fixture[offset:offset+size]...)
		}
		offset += size
	}
	return append(output, metadata...)
}

func replaceOGGCommentPacket(t *testing.T, fixture, comments []byte) []byte {
	t.Helper()
	for offset := 0; offset < len(fixture); {
		segmentCount := int(fixture[offset+26])
		laces := fixture[offset+27 : offset+27+segmentCount]
		start, end := offset+27+segmentCount, offset+27+segmentCount
		for _, size := range laces {
			end += int(size)
		}
		prefix, suffix := []byte("OpusTags"), []byte(nil)
		if bytes.HasPrefix(fixture[start:end], []byte("\x03vorbis")) {
			prefix, suffix = []byte("\x03vorbis"), []byte{1}
		}
		if bytes.HasPrefix(fixture[start:end], prefix) {
			packetEnd, packetSegments := start, 0
			for _, size := range laces {
				packetEnd += int(size)
				packetSegments++
				if size < 255 {
					break
				}
			}
			packet := append(append(prefix, comments...), suffix...)
			newLaces := bytes.Repeat([]byte{255}, len(packet)/255)
			newLaces = append(newLaces, byte(len(packet)%255))
			newLaces = append(newLaces, laces[packetSegments:]...)
			page := append([]byte(nil), fixture[offset:offset+27]...)
			page[26] = byte(len(newLaces))
			page = append(page, newLaces...)
			page = append(page, packet...)
			page = append(page, fixture[packetEnd:end]...)
			result := append(append(append([]byte(nil), fixture[:offset]...), page...), fixture[end:]...)
			updateOGGChecksums(t, result)
			return result
		}
		offset = end
	}
	t.Fatal("fixture has no complete comment packet")
	return nil
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

func TestMediaInspectorInspectsStrictOGGFixtures(t *testing.T) {
	tests := []struct {
		name         string
		filename     string
		format       string
		codec        string
		expectedHash string
		sampleRateHz int
		bitrateKbps  int
	}{
		{name: "Vorbis", filename: "strict-import.ogg", format: "ogg", codec: "vorbis", expectedHash: "7d61b6f5fda0f02392177282bc79c4b96e33736ec41056891dff4de31fbc7f6a", sampleRateHz: 44100, bitrateKbps: 20},
		{name: "Opus", filename: "strict-import.opus", format: "opus", codec: "opus", expectedHash: "6ba8a2b924835bec5cbb97d538b8077dd4d722faa15baf83a0139ad4a27867d4", sampleRateHz: 48000, bitrateKbps: 85},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var progress []library.InspectionProgress
			inspection, err := library.NewMediaInspector().Inspect(context.Background(), filepath.Join("testdata", testCase.filename), func(update library.InspectionProgress) error {
				progress = append(progress, update)
				return nil
			})
			if err != nil {
				t.Fatalf("inspect %s fixture: %v", testCase.name, err)
			}
			assertOGGMetadata(t, inspection.Metadata)
			assertAlbumArtwork(t, inspection.AlbumArtwork)
			if inspection.Audio.Format != testCase.format || inspection.Audio.Container != "ogg" || inspection.Audio.Codec != testCase.codec {
				t.Fatalf("format/container/codec = %s/%s/%s", inspection.Audio.Format, inspection.Audio.Container, inspection.Audio.Codec)
			}
			if inspection.Audio.DurationMs != 250 || inspection.Audio.SampleRateHz != testCase.sampleRateHz || inspection.Audio.ChannelCount != 1 {
				t.Fatalf("duration/sample rate/channels = %d/%d/%d", inspection.Audio.DurationMs, inspection.Audio.SampleRateHz, inspection.Audio.ChannelCount)
			}
			if inspection.Audio.BitDepth != 0 || inspection.Audio.BitrateKbps != testCase.bitrateKbps {
				t.Fatalf("bit depth/bitrate = %d/%d", inspection.Audio.BitDepth, inspection.Audio.BitrateKbps)
			}
			if inspection.FileSHA256 != testCase.expectedHash {
				t.Fatalf("file SHA-256 = %q", inspection.FileSHA256)
			}
			assertCompletedInspectionProgress(t, progress)
		})
	}
}

func assertOGGMetadata(t *testing.T, metadata library.NormalizedMediaMetadata) {
	t.Helper()
	if metadata.Title != "OGG Inspection Fixture" || metadata.Album != "Strict OGG Import Tests" {
		t.Fatalf("title/album = %q/%q", metadata.Title, metadata.Album)
	}
	if !reflect.DeepEqual(metadata.Artists, []string{"First Artist", "Second Artist"}) {
		t.Fatalf("artists = %#v", metadata.Artists)
	}
	if !reflect.DeepEqual(metadata.AlbumArtists, []string{"Test Album Artist"}) {
		t.Fatalf("album artists = %#v", metadata.AlbumArtists)
	}
	if !reflect.DeepEqual(metadata.Genres, []string{"Electronic", "Ambient"}) {
		t.Fatalf("genres = %#v", metadata.Genres)
	}
	if metadata.TrackPosition != (library.MediaPosition{Number: 3, Total: 9}) || metadata.DiscPosition != (library.MediaPosition{Number: 1, Total: 1}) || metadata.Year != 2026 {
		t.Fatalf("positions/year = %+v/%+v/%d", metadata.TrackPosition, metadata.DiscPosition, metadata.Year)
	}
}

func TestMediaInspectorReturnsStableOGGErrors(t *testing.T) {
	tests := []struct {
		name          string
		filename      string
		mutate        func(*testing.T, []byte) []byte
		expectedCode  library.InspectionErrorCode
		expectedField string
	}{
		{name: "malformed Vorbis comment", filename: "strict-import.ogg", mutate: replaceOGGBytes([]byte("title="), []byte("title_")), expectedCode: library.INSPECTION_ERROR_INVALID_METADATA, expectedField: "comments"},
		{name: "malformed Opus comment", filename: "strict-import.opus", mutate: replaceOGGBytes([]byte("title="), []byte("title_")), expectedCode: library.INSPECTION_ERROR_INVALID_METADATA, expectedField: "comments"},
		{name: "unsupported OGG stream", filename: "strict-import.opus", mutate: replaceOGGBytes([]byte("OpusHead"), []byte("NopeHead")), expectedCode: library.INSPECTION_ERROR_UNSUPPORTED_FORMAT, expectedField: "container"},
		{name: "truncated Vorbis stream", filename: "strict-import.ogg", mutate: truncateOGGEnd, expectedCode: library.INSPECTION_ERROR_AUDIO_DECODE, expectedField: "audio"},
		{name: "truncated Opus stream", filename: "strict-import.opus", mutate: truncateOGGEnd, expectedCode: library.INSPECTION_ERROR_AUDIO_DECODE, expectedField: "audio"},
		{name: "corrupt page after Vorbis EOS", filename: "strict-import.ogg", mutate: appendCorruptOGGPage, expectedCode: library.INSPECTION_ERROR_AUDIO_DECODE, expectedField: "audio"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := readOGGFixture(t, testCase.filename)
			path := filepath.Join(t.TempDir(), testCase.filename)
			if err := os.WriteFile(path, testCase.mutate(t, fixture), 0o600); err != nil {
				t.Fatalf("write mutated OGG fixture: %v", err)
			}
			_, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
			var inspectionErr *library.InspectionError
			if !errors.As(err, &inspectionErr) {
				t.Fatalf("error = %T %v", err, err)
			}
			if inspectionErr.Code != testCase.expectedCode || inspectionErr.Field != testCase.expectedField {
				t.Fatalf("inspection error = %+v", inspectionErr)
			}
		})
	}
}

func TestMediaInspectorAppliesFLACArtworkRulesToOGG(t *testing.T) {
	tests := []struct {
		name         string
		filename     string
		mutate       func(*testing.T, []byte) []byte
		expectedCode library.InspectionErrorCode
	}{
		{name: "Vorbis missing front cover", filename: "strict-import.ogg", mutate: replaceOGGBytes([]byte("metadata_block_picture"), []byte("xetadata_block_picture")), expectedCode: library.INSPECTION_ERROR_MISSING_ARTWORK},
		{name: "Opus missing front cover", filename: "strict-import.opus", mutate: replaceOGGBytes([]byte("metadata_block_picture"), []byte("xetadata_block_picture")), expectedCode: library.INSPECTION_ERROR_MISSING_ARTWORK},
		{name: "Vorbis malformed picture", filename: "strict-import.ogg", mutate: replaceOGGBytes([]byte("metadata_block_picture=A"), []byte("metadata_block_picture=!")), expectedCode: library.INSPECTION_ERROR_INVALID_ARTWORK},
		{name: "Opus malformed picture", filename: "strict-import.opus", mutate: replaceOGGBytes([]byte("metadata_block_picture=A"), []byte("metadata_block_picture=!")), expectedCode: library.INSPECTION_ERROR_INVALID_ARTWORK},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), testCase.filename)
			fixture := testCase.mutate(t, readOGGFixture(t, testCase.filename))
			if err := os.WriteFile(path, fixture, 0o600); err != nil {
				t.Fatalf("write artwork OGG fixture: %v", err)
			}
			inspection, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
			if err != nil || len(inspection.AlbumArtwork.Data) != 0 {
				t.Fatalf("optional artwork = %+v, %v", inspection.AlbumArtwork, err)
			}
			if testCase.expectedCode == library.INSPECTION_ERROR_INVALID_ARTWORK && inspection.AlbumArtwork.Warning == "" {
				t.Fatal("missing invalid artwork warning")
			}
		})
	}
}

func replaceOGGBytes(oldValue, newValue []byte) func(*testing.T, []byte) []byte {
	return func(t *testing.T, fixture []byte) []byte {
		t.Helper()
		if len(oldValue) != len(newValue) || !bytes.Contains(fixture, oldValue) {
			t.Fatalf("cannot replace OGG fixture value %q", oldValue)
		}
		result := bytes.Replace(append([]byte(nil), fixture...), oldValue, newValue, 1)
		updateOGGChecksums(t, result)
		return result
	}
}

func truncateOGGEnd(t *testing.T, fixture []byte) []byte {
	t.Helper()
	return append([]byte(nil), fixture[:len(fixture)-8]...)
}

func appendCorruptOGGPage(t *testing.T, fixture []byte) []byte {
	t.Helper()
	lastPageOffset := 0
	for offset := 0; offset < len(fixture); {
		lastPageOffset = offset
		segmentCount := int(fixture[offset+OGG_PAGE_SEGMENT_COUNT_OFFSET])
		segmentTableEnd := offset + library.OGG_PAGE_HEADER_SIZE_BYTES + segmentCount
		offset = segmentTableEnd
		for _, size := range fixture[lastPageOffset+library.OGG_PAGE_HEADER_SIZE_BYTES : segmentTableEnd] {
			offset += int(size)
		}
	}
	result := append([]byte(nil), fixture...)
	result = append(result, fixture[lastPageOffset:]...)
	result[len(result)-1] ^= 0xff
	return result
}

func readOGGFixture(t *testing.T, filename string) []byte {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("testdata", filename))
	if err != nil {
		t.Fatalf("read OGG fixture: %v", err)
	}
	return fixture
}

func updateOGGChecksums(t *testing.T, fixture []byte) {
	t.Helper()
	for offset := 0; offset < len(fixture); {
		if offset+library.OGG_PAGE_HEADER_SIZE_BYTES > len(fixture) || string(fixture[offset:offset+len(library.OGG_SIGNATURE)]) != library.OGG_SIGNATURE {
			t.Fatal("OGG fixture page header is invalid")
		}
		segmentCount := int(fixture[offset+OGG_PAGE_SEGMENT_COUNT_OFFSET])
		segmentTableEnd := offset + library.OGG_PAGE_HEADER_SIZE_BYTES + segmentCount
		if segmentTableEnd > len(fixture) {
			t.Fatal("OGG fixture segment table is truncated")
		}
		pageEnd := segmentTableEnd
		for _, size := range fixture[offset+library.OGG_PAGE_HEADER_SIZE_BYTES : segmentTableEnd] {
			pageEnd += int(size)
		}
		if pageEnd > len(fixture) {
			t.Fatal("OGG fixture page is truncated")
		}
		clear(fixture[offset+OGG_CHECKSUM_OFFSET : offset+OGG_CHECKSUM_OFFSET+OGG_CHECKSUM_SIZE_BYTES])
		binary.LittleEndian.PutUint32(fixture[offset+OGG_CHECKSUM_OFFSET:], oggChecksum(fixture[offset:pageEnd]))
		offset = pageEnd
	}
}

func oggChecksum(data []byte) uint32 {
	var checksum uint32
	for _, value := range data {
		checksum ^= uint32(value) << 24
		for range 8 {
			if checksum&0x80000000 != 0 {
				checksum = checksum<<1 ^ OGG_CHECKSUM_POLYNOMIAL
			} else {
				checksum <<= 1
			}
		}
	}
	return checksum
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

func TestMediaInspectorAcceptsSupportedStaticArtworkFormats(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		picture embeddedPicture
	}{
		{name: "JPEG", picture: frontCover("image/jpeg", encodeJPEG(t))},
		{name: "PNG", picture: frontCover("image/png", encodePNG(t))},
		{name: "WebP", picture: frontCover("image/webp", decodeBase64(t, "UklGRjwAAABXRUJQVlA4IDAAAADQAQCdASoCAAIAAgA0JaACdLoB+AADsAD+8MQL/yC5YXXI1/8gP+QH/ID/+PIAAAA="))},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeFLACWithPictures(t, []embeddedPicture{testCase.picture})

			inspection, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
			if err != nil {
				t.Fatalf("inspect %s artwork: %v", testCase.name, err)
			}
			if inspection.AlbumArtwork.MIMEType != testCase.picture.mimeType || inspection.AlbumArtwork.Width != TEST_ARTWORK_WIDTH || inspection.AlbumArtwork.Height != TEST_ARTWORK_HEIGHT {
				t.Fatalf("%s artwork = %+v", testCase.name, inspection.AlbumArtwork)
			}
		})
	}
}

func TestMediaInspectorRejectsUnsafeEmbeddedArtwork(t *testing.T) {
	pngData := encodePNG(t)
	testCases := []struct {
		name         string
		pictures     []embeddedPicture
		expectedCode library.InspectionErrorCode
	}{
		{name: "missing front cover", expectedCode: library.INSPECTION_ERROR_MISSING_ARTWORK},
		{name: "generic picture is not a front cover", pictures: []embeddedPicture{genericCover("image/png", pngData)}, expectedCode: library.INSPECTION_ERROR_MISSING_ARTWORK},
		{name: "multiple generic pictures are not front covers", pictures: []embeddedPicture{genericCover("image/png", pngData), genericCover("image/png", pngData)}, expectedCode: library.INSPECTION_ERROR_MISSING_ARTWORK},
		{name: "ambiguous front covers", pictures: []embeddedPicture{frontCover("image/png", pngData), frontCover("image/png", pngData)}, expectedCode: library.INSPECTION_ERROR_INVALID_ARTWORK},
		{name: "declared type mismatch", pictures: []embeddedPicture{frontCover("image/jpeg", pngData)}, expectedCode: library.INSPECTION_ERROR_INVALID_ARTWORK},
		{name: "truncated image", pictures: []embeddedPicture{frontCover("image/png", pngData[:len(pngData)-TRUNCATED_PNG_SUFFIX_BYTES])}, expectedCode: library.INSPECTION_ERROR_INVALID_ARTWORK},
		{name: "animated PNG", pictures: []embeddedPicture{frontCover("image/png", addPNGAnimationControl(t, pngData))}, expectedCode: library.INSPECTION_ERROR_INVALID_ARTWORK},
		{name: "animated WebP", pictures: []embeddedPicture{frontCover("image/webp", animatedWebP(t))}, expectedCode: library.INSPECTION_ERROR_INVALID_ARTWORK},
		{name: "decoded pixel limit", pictures: []embeddedPicture{frontCover("image/png", oversizedPNG(t, pngData))}, expectedCode: library.INSPECTION_ERROR_INVALID_ARTWORK},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeFLACWithPictures(t, testCase.pictures)

			inspection, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
			if err != nil || len(inspection.AlbumArtwork.Data) != 0 || inspection.AlbumArtwork.SHA256 != "" {
				t.Fatalf("unsafe artwork retained: %+v, %v", inspection.AlbumArtwork, err)
			}
			if testCase.expectedCode == library.INSPECTION_ERROR_INVALID_ARTWORK && inspection.AlbumArtwork.Warning == "" {
				t.Fatal("missing invalid artwork warning")
			}
		})
	}
}

type embeddedPicture struct {
	pictureType uint32
	mimeType    string
	data        []byte
}

func frontCover(mimeType string, data []byte) embeddedPicture {
	return embeddedPicture{pictureType: library.FLAC_PICTURE_TYPE_FRONT_COVER, mimeType: mimeType, data: data}
}

func genericCover(mimeType string, data []byte) embeddedPicture {
	return embeddedPicture{pictureType: library.FLAC_PICTURE_TYPE_OTHER, mimeType: mimeType, data: data}
}

type flacMetadataBlock struct {
	blockType byte
	body      []byte
}

func writeFLACWithPictures(t *testing.T, pictures []embeddedPicture) string {
	t.Helper()
	fixture := readInspectionFixture(t)
	blocks, audio := splitFLACMetadata(t, fixture)
	output := encodeFLACFixture(t, replaceFLACPictureBlocks(t, blocks, pictures), audio)
	path := filepath.Join(t.TempDir(), "artwork.flac")
	if err := os.WriteFile(path, output, 0o600); err != nil {
		t.Fatalf("write artwork FLAC: %v", err)
	}
	return path
}

func replaceFLACPictureBlocks(t *testing.T, blocks []flacMetadataBlock, pictures []embeddedPicture) []flacMetadataBlock {
	t.Helper()
	rewritten := make([]flacMetadataBlock, 0, len(blocks)+len(pictures))
	picturesInserted := false
	for _, block := range blocks {
		if block.blockType != byte(flacmeta.TypePicture) {
			rewritten = append(rewritten, block)
			continue
		}
		if picturesInserted {
			continue
		}
		for _, picture := range pictures {
			rewritten = append(rewritten, flacMetadataBlock{blockType: byte(flacmeta.TypePicture), body: encodeFLACPicture(t, picture)})
		}
		picturesInserted = true
	}
	if !picturesInserted {
		t.Fatal("strict FLAC fixture has no Picture block")
	}
	return rewritten
}

func encodeFLACFixture(t *testing.T, blocks []flacMetadataBlock, audio []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	output.WriteString(library.FLAC_SIGNATURE)
	for index, block := range blocks {
		header := block.blockType
		if index == len(blocks)-1 {
			header |= FLAC_METADATA_BLOCK_LAST_FLAG
		}
		output.WriteByte(header)
		length := len(block.body)
		output.Write([]byte{byte(length >> FLAC_METADATA_LENGTH_HIGH_SHIFT), byte(length >> FLAC_METADATA_LENGTH_MIDDLE_SHIFT), byte(length)})
		output.Write(block.body)
	}
	output.Write(audio)
	return output.Bytes()
}

func splitFLACMetadata(t *testing.T, fixture []byte) ([]flacMetadataBlock, []byte) {
	t.Helper()
	if len(fixture) < library.FLAC_SIGNATURE_SIZE_BYTES || string(fixture[:library.FLAC_SIGNATURE_SIZE_BYTES]) != library.FLAC_SIGNATURE {
		t.Fatal("strict FLAC fixture signature is invalid")
	}
	offset := library.FLAC_SIGNATURE_SIZE_BYTES
	var blocks []flacMetadataBlock
	for {
		if offset+library.FLAC_METADATA_BLOCK_HEADER_SIZE_BYTES > len(fixture) {
			t.Fatal("strict FLAC fixture metadata is truncated")
		}
		header := fixture[offset]
		length := int(fixture[offset+FLAC_METADATA_LENGTH_HIGH_BYTE_OFFSET])<<FLAC_METADATA_LENGTH_HIGH_SHIFT |
			int(fixture[offset+FLAC_METADATA_LENGTH_MIDDLE_BYTE_OFFSET])<<FLAC_METADATA_LENGTH_MIDDLE_SHIFT |
			int(fixture[offset+FLAC_METADATA_LENGTH_LOW_BYTE_OFFSET])
		offset += library.FLAC_METADATA_BLOCK_HEADER_SIZE_BYTES
		if offset+length > len(fixture) {
			t.Fatal("strict FLAC fixture block is truncated")
		}
		blocks = append(blocks, flacMetadataBlock{blockType: header & FLAC_METADATA_BLOCK_TYPE_MASK, body: append([]byte(nil), fixture[offset:offset+length]...)})
		offset += length
		if header&FLAC_METADATA_BLOCK_LAST_FLAG != 0 {
			return blocks, fixture[offset:]
		}
	}
}

func encodeFLACPicture(t *testing.T, picture embeddedPicture) []byte {
	t.Helper()
	var output bytes.Buffer
	values := []uint32{picture.pictureType, uint32(len(picture.mimeType))}
	for _, value := range values {
		if err := binary.Write(&output, binary.BigEndian, value); err != nil {
			t.Fatalf("encode FLAC Picture: %v", err)
		}
	}
	output.WriteString(picture.mimeType)
	for _, value := range []uint32{0, TEST_ARTWORK_WIDTH, TEST_ARTWORK_HEIGHT, TEST_ARTWORK_DEPTH, 0, uint32(len(picture.data))} {
		if err := binary.Write(&output, binary.BigEndian, value); err != nil {
			t.Fatalf("encode FLAC Picture: %v", err)
		}
	}
	output.Write(picture.data)
	return output.Bytes()
}

func encodePNG(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := png.Encode(&output, testImage()); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return output.Bytes()
}

func encodeJPEG(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := jpeg.Encode(&output, testImage(), nil); err != nil {
		t.Fatalf("encode JPEG: %v", err)
	}
	return output.Bytes()
}

func testImage() image.Image {
	result := image.NewRGBA(image.Rect(0, 0, 2, 2))
	result.Set(0, 0, color.RGBA{R: 255, A: 255})
	result.Set(1, 0, color.RGBA{G: 255, A: 255})
	result.Set(0, 1, color.RGBA{B: 255, A: 255})
	result.Set(1, 1, color.RGBA{R: 255, G: 255, A: 255})
	return result
}

func decodeBase64(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	return decoded
}

func addPNGAnimationControl(t *testing.T, data []byte) []byte {
	t.Helper()
	idatOffset := bytes.Index(data, []byte("IDAT")) - PNG_CHUNK_LENGTH_SIZE_BYTES
	if idatOffset < len(library.PNG_SIGNATURE) {
		t.Fatal("PNG has no IDAT chunk")
	}
	chunkData := make([]byte, PNG_CHUNK_LENGTH_SIZE_BYTES*PNG_ANIMATION_CONTROL_FIELD_COUNT)
	binary.BigEndian.PutUint32(chunkData[:PNG_CHUNK_LENGTH_SIZE_BYTES], PNG_ANIMATION_FRAME_COUNT)
	chunk := encodePNGChunk(t, library.PNG_ANIMATION_CHUNK, chunkData)
	result := append([]byte(nil), data[:idatOffset]...)
	result = append(result, chunk...)
	return append(result, data[idatOffset:]...)
}

func encodePNGChunk(t *testing.T, chunkType string, data []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := binary.Write(&output, binary.BigEndian, uint32(len(data))); err != nil {
		t.Fatalf("encode PNG chunk length: %v", err)
	}
	output.WriteString(chunkType)
	output.Write(data)
	checksum := crc32.ChecksumIEEE(output.Bytes()[PNG_CHUNK_LENGTH_SIZE_BYTES:])
	if err := binary.Write(&output, binary.BigEndian, checksum); err != nil {
		t.Fatalf("encode PNG chunk checksum: %v", err)
	}
	return output.Bytes()
}

func oversizedPNG(t *testing.T, data []byte) []byte {
	t.Helper()
	if len(data) < PNG_IHDR_CRC_OFFSET+PNG_CHUNK_LENGTH_SIZE_BYTES || string(data[PNG_IHDR_CRC_INPUT_OFFSET:PNG_IHDR_WIDTH_OFFSET]) != "IHDR" {
		t.Fatal("PNG has no IHDR chunk")
	}
	result := append([]byte(nil), data...)
	binary.BigEndian.PutUint32(result[PNG_IHDR_WIDTH_OFFSET:PNG_IHDR_HEIGHT_OFFSET], OVERSIZED_ARTWORK_WIDTH)
	binary.BigEndian.PutUint32(result[PNG_IHDR_HEIGHT_OFFSET:PNG_IHDR_HEIGHT_OFFSET+PNG_CHUNK_LENGTH_SIZE_BYTES], OVERSIZED_ARTWORK_HEIGHT)
	binary.BigEndian.PutUint32(result[PNG_IHDR_CRC_OFFSET:PNG_IHDR_CRC_OFFSET+PNG_CHUNK_LENGTH_SIZE_BYTES], crc32.ChecksumIEEE(result[PNG_IHDR_CRC_INPUT_OFFSET:PNG_IHDR_CRC_OFFSET]))
	return result
}

func animatedWebP(t *testing.T) []byte {
	t.Helper()
	return decodeBase64(t, ANIMATED_WEBP_FIXTURE_BASE64)
}

func readInspectionFixture(t *testing.T) []byte {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("testdata", "strict-import.flac"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return fixture
}

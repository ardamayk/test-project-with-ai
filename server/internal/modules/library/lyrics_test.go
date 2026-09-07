package library_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	flacmeta "github.com/mewkiz/flac/meta"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

func TestMediaInspectorReadsVorbisLyrics(t *testing.T) {
	path := writeFLACWithExtraVorbisTags(t, [][2]string{{"LYRICS", "First line\r\nSecond line\n"}})

	inspection, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("inspect FLAC with lyrics: %v", err)
	}
	if inspection.Metadata.Lyrics != "First line\nSecond line" {
		t.Fatalf("lyrics = %q", inspection.Metadata.Lyrics)
	}
}

func TestMediaInspectorAcceptsUnsyncedLyricsKey(t *testing.T) {
	path := writeFLACWithExtraVorbisTags(t, [][2]string{{"UNSYNCEDLYRICS", "Only line"}})

	inspection, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("inspect FLAC with unsynced lyrics: %v", err)
	}
	if inspection.Metadata.Lyrics != "Only line" {
		t.Fatalf("lyrics = %q", inspection.Metadata.Lyrics)
	}
}

func TestMediaInspectorLeavesLyricsEmptyWhenUntagged(t *testing.T) {
	inspection, err := library.NewMediaInspector().Inspect(context.Background(), filepath.Join("testdata", "strict-import.flac"), nil)
	if err != nil {
		t.Fatalf("inspect fixture: %v", err)
	}
	if inspection.Metadata.Lyrics != "" {
		t.Fatalf("lyrics = %q, want empty", inspection.Metadata.Lyrics)
	}
}

func TestMediaInspectorReadsID3Lyrics(t *testing.T) {
	for _, version := range []byte{3, 4} {
		fixture := testutil.StrictMP3FixtureWithExtraFrames(version, testutil.ID3LyricsFrame(version, "eng", "", "Verse one\nVerse two"))
		inspection := inspectMP3Fixture(t, fixture)
		if inspection.Metadata.Lyrics != "Verse one\nVerse two" {
			t.Fatalf("ID3v2.%d lyrics = %q", version, inspection.Metadata.Lyrics)
		}
	}
}

// writeFLACWithExtraVorbisTags appends Vorbis comments to the strict FLAC
// fixture and writes the result to a temporary file.
func writeFLACWithExtraVorbisTags(t *testing.T, extra [][2]string) string {
	t.Helper()
	blocks, audio := splitFLACMetadata(t, readInspectionFixture(t))
	rewritten := make([]flacMetadataBlock, 0, len(blocks))
	found := false
	for _, block := range blocks {
		if block.blockType == byte(flacmeta.TypeVorbisComment) {
			block = flacMetadataBlock{blockType: block.blockType, body: appendVorbisComments(t, block.body, extra)}
			found = true
		}
		rewritten = append(rewritten, block)
	}
	if !found {
		t.Fatal("strict FLAC fixture has no Vorbis comment block")
	}
	path := filepath.Join(t.TempDir(), "lyrics.flac")
	if err := os.WriteFile(path, encodeFLACFixture(t, rewritten, audio), 0o600); err != nil {
		t.Fatalf("write FLAC with lyrics: %v", err)
	}
	return path
}

func appendVorbisComments(t *testing.T, body []byte, extra [][2]string) []byte {
	t.Helper()
	if len(body) < 4 {
		t.Fatal("Vorbis comment block is truncated")
	}
	vendorLength := int(binary.LittleEndian.Uint32(body[:4]))
	countOffset := 4 + vendorLength
	count := int(binary.LittleEndian.Uint32(body[countOffset : countOffset+4]))
	var output bytes.Buffer
	output.Write(body[:countOffset])
	sizeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBytes, uint32(count+len(extra)))
	output.Write(sizeBytes)
	output.Write(body[countOffset+4:])
	for _, tag := range extra {
		entry := tag[0] + "=" + tag[1]
		binary.LittleEndian.PutUint32(sizeBytes, uint32(len(entry)))
		output.Write(sizeBytes)
		output.WriteString(entry)
	}
	return output.Bytes()
}

package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

func strictVorbisTags() map[string][]string {
	return map[string][]string{
		"TITLE": {"Song"}, "ARTIST": {"Artist"}, "ALBUMARTIST": {"Artist"}, "ALBUM": {"Album"},
		"TRACKNUMBER": {"1"}, "GENRE": {"Pop"},
	}
}

func TestNormalizeMediaMetadataReadsOptionalIdentifiers(t *testing.T) {
	tags := strictVorbisTags()
	tags["ISRC"] = []string{" usug12306672 "}
	tags["MUSICBRAINZ_TRACKID"] = []string{"2872B086-7C8D-4B3A-9B91-55805CD912B0"}

	metadata, err := normalizeMediaMetadata(tags, ReplayGainMetadata{})
	if err != nil {
		t.Fatalf("normalizeMediaMetadata() error = %v", err)
	}

	if metadata.ISRC != "USUG12306672" {
		t.Fatalf("ISRC = %q", metadata.ISRC)
	}
	if metadata.MusicBrainzRecordingID != "2872b086-7c8d-4b3a-9b91-55805cd912b0" {
		t.Fatalf("MusicBrainzRecordingID = %q", metadata.MusicBrainzRecordingID)
	}
}

func TestNormalizeMediaMetadataIgnoresMalformedIdentifiers(t *testing.T) {
	tags := strictVorbisTags()
	tags["ISRC"] = []string{"not-an-isrc"}
	tags["MUSICBRAINZ_TRACKID"] = []string{"12345"}

	metadata, err := normalizeMediaMetadata(tags, ReplayGainMetadata{})
	if err != nil {
		t.Fatalf("malformed optional identifiers must not reject the file: %v", err)
	}
	if metadata.ISRC != "" || metadata.MusicBrainzRecordingID != "" {
		t.Fatalf("malformed identifiers must be dropped: %+v", metadata)
	}
}

func TestNormalizeMediaMetadataAcceptsRecordingIDAlias(t *testing.T) {
	tags := strictVorbisTags()
	tags["MUSICBRAINZ_RECORDINGID"] = []string{"2872b086-7c8d-4b3a-9b91-55805cd912b0"}

	metadata, err := normalizeMediaMetadata(tags, ReplayGainMetadata{})
	if err != nil || metadata.MusicBrainzRecordingID != "2872b086-7c8d-4b3a-9b91-55805cd912b0" {
		t.Fatalf("metadata = %+v err = %v", metadata, err)
	}
}

func TestMediaInspectorReadsMP3ISRCAndMusicBrainzUFID(t *testing.T) {
	for _, version := range []byte{3, 4} {
		fixture := testutil.StrictMP3FixtureWithExtraFrames(version,
			testutil.ID3TextFrame(version, "TSRC", "USUG12306672"),
			testutil.ID3UFIDFrame(version, "http://musicbrainz.org", "2872b086-7c8d-4b3a-9b91-55805cd912b0"),
			testutil.ID3UFIDFrame(version, "http://example.org/other", "ignored"),
		)
		path := filepath.Join(t.TempDir(), "tagged.mp3")
		if err := os.WriteFile(path, fixture, 0o600); err != nil {
			t.Fatal(err)
		}

		inspection, err := NewMediaInspector().Inspect(context.Background(), path, nil)
		if err != nil {
			t.Fatalf("v2.%d Inspect() error = %v", version, err)
		}
		if inspection.Metadata.ISRC != "USUG12306672" || inspection.Metadata.MusicBrainzRecordingID != "2872b086-7c8d-4b3a-9b91-55805cd912b0" {
			t.Fatalf("v2.%d identifiers = %q %q", version, inspection.Metadata.ISRC, inspection.Metadata.MusicBrainzRecordingID)
		}
	}
}

func TestM4AMetadataKeyMapsMusicBrainzAndISRCTags(t *testing.T) {
	if key := m4aMetadataKey("MusicBrainz Track Id"); key != "MUSICBRAINZ_TRACKID" {
		t.Fatalf("MusicBrainz Track Id -> %q", key)
	}
	if key := m4aMetadataKey("isrc"); key != "ISRC" {
		t.Fatalf("isrc -> %q", key)
	}
}

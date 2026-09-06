package library_test

import (
	"encoding/binary"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

func TestMediaInspectorAcceptsMP3APEv2Boundaries(t *testing.T) {
	for _, hasHeader := range []bool{false, true} {
		for _, hasID3v1 := range []bool{false, true} {
			fixture := append(testutil.StrictMP3Fixture(), apev2Fixture(hasHeader)...)
			if hasID3v1 {
				fixture = append(fixture, append([]byte("TAG"), make([]byte, 125)...)...)
			}
			inspection := inspectMP3Fixture(t, fixture)
			if inspection.Metadata.Title != "MP3 Inspection Fixture" {
				t.Fatalf("APEv2 replaced ID3 title: %q", inspection.Metadata.Title)
			}
		}
	}
}

func apev2Fixture(hasHeader bool) []byte {
	item := append([]byte{9, 0, 0, 0, 0, 0, 0, 0}, []byte("Title\x00APE title")...)
	footer := make([]byte, 32)
	copy(footer, "APETAGEX")
	binary.LittleEndian.PutUint32(footer[8:12], 2000)
	binary.LittleEndian.PutUint32(footer[12:16], uint32(len(item)+32))
	binary.LittleEndian.PutUint32(footer[16:20], 1)
	if !hasHeader {
		return append(item, footer...)
	}
	binary.LittleEndian.PutUint32(footer[20:24], 1<<31)
	header := append([]byte{}, footer...)
	binary.LittleEndian.PutUint32(header[20:24], 1<<31|1<<29)
	return append(append(header, item...), footer...)
}

func TestMediaInspectorRejectsMalformedMP3APEv2(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		value  uint32
	}{
		{"version", 8, 1000}, {"undersized", 12, 31}, {"out of bounds", 12, ^uint32(0)},
		{"item count", 16, ^uint32(0)}, {"no footer", 20, 1 << 30},
		{"footer marked header", 20, 1 << 29}, {"reserved bytes", 24, 1},
		{"header missing", 20, 1 << 31},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			tag := apev2Fixture(false)
			binary.LittleEndian.PutUint32(tag[len(tag)-32+testCase.offset:], testCase.value)
			_, err := inspectMP3FixtureError(t, append(testutil.StrictMP3Fixture(), tag...))
			if err == nil {
				t.Fatal("malformed APEv2 accepted")
			}
		})
	}
	t.Run("header mismatch", func(t *testing.T) {
		tag := apev2Fixture(true)
		binary.LittleEndian.PutUint32(tag[16:20], 0)
		_, err := inspectMP3FixtureError(t, append(testutil.StrictMP3Fixture(), tag...))
		if err == nil {
			t.Fatal("mismatched APEv2 accepted")
		}
	})
	t.Run("damaged audio before valid tag", func(t *testing.T) {
		audio := testutil.StrictMP3Fixture()
		_, err := inspectMP3FixtureError(t, append(audio[:len(audio)-8], apev2Fixture(true)...))
		if err == nil {
			t.Fatal("truncated audio accepted")
		}
	})
}

func TestMediaInspectorRejectsAPEv2ItemOutsideBlock(t *testing.T) {
	tag := apev2Fixture(false)
	binary.LittleEndian.PutUint32(tag[:4], 1000)
	_, err := inspectMP3FixtureError(t, append(testutil.StrictMP3Fixture(), tag...))
	if err == nil {
		t.Fatal("APEv2 item extending past footer accepted")
	}
}

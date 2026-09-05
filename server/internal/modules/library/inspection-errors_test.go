package library_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

func TestMediaInspectorReportsIndependentFLACErrors(t *testing.T) {
	blocks, audio := splitFLACMetadata(t, readInspectionFixture(t))
	fixture := encodeFLACFixture(t, replaceFLACPictureBlocks(t, blocks, nil), audio)
	for _, key := range []string{"TITLE", "GENRE", "TRACKNUMBER"} {
		fixture = bytes.ReplaceAll(fixture, []byte(key+"="), []byte("X"+key[1:]+"="))
	}
	path := filepath.Join(t.TempDir(), "multiple-errors.flac")
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
	issues := library.InspectionIssues(err)
	fields := []string{}
	for _, issue := range issues {
		fields = append(fields, issue.Field)
	}
	if !reflect.DeepEqual(fields, []string{"TITLE", "TRACKNUMBER"}) {
		t.Fatalf("validation fields = %v; error = %v", fields, err)
	}
}

func TestMediaInspectorReportsMP3MetadataArtworkAndAudioErrors(t *testing.T) {
	fixture := testutil.StrictMP3Fixture()
	fixture = bytes.ReplaceAll(fixture, []byte("TIT2"), []byte("XXXX"))
	fixture = bytes.ReplaceAll(fixture, []byte("TCON"), []byte("YYYY"))
	fixture = bytes.ReplaceAll(fixture, []byte("APIC"), []byte("ZZZZ"))
	_, err := inspectMP3FixtureError(t, fixture[:len(fixture)-8])
	fields := []string{}
	for _, issue := range library.InspectionIssues(err) {
		fields = append(fields, issue.Field)
	}
	if !reflect.DeepEqual(fields, []string{"TITLE", "audio"}) {
		t.Fatalf("validation fields = %v; error = %v", fields, err)
	}
}

func TestMediaInspectorReportsIndependentOGGErrors(t *testing.T) {
	for _, filename := range []string{"strict-import.ogg", "strict-import.opus"} {
		t.Run(filename, func(t *testing.T) {
			fixture := readOGGFixture(t, filename)
			fixture = replaceOGGBytes([]byte("title="), []byte("xitle="))(t, fixture)
			fixture = replaceOGGBytes([]byte("metadata_block_picture"), []byte("xetadata_block_picture"))(t, fixture)
			path := filepath.Join(t.TempDir(), filename)
			if err := os.WriteFile(path, fixture, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := library.NewMediaInspector().Inspect(context.Background(), path, nil)
			fields := []string{}
			for _, issue := range library.InspectionIssues(err) {
				fields = append(fields, issue.Field)
			}
			if !reflect.DeepEqual(fields, []string{"TITLE"}) {
				t.Fatalf("validation fields = %v", fields)
			}
		})
	}
}

func TestMediaInspectorContinuesAfterMalformedID3Field(t *testing.T) {
	fixture := testutil.StrictMP3Fixture()
	titleOffset := bytes.Index(fixture, []byte("TIT2"))
	if titleOffset < 0 {
		t.Fatal("missing TITLE fixture frame")
	}
	fixture[titleOffset+10] = 0xff
	fixture = bytes.ReplaceAll(fixture, []byte("TCON"), []byte("XXXX"))
	fixture = bytes.ReplaceAll(fixture, []byte("APIC"), []byte("YYYY"))
	_, err := inspectMP3FixtureError(t, fixture)
	fields := []string{}
	for _, issue := range library.InspectionIssues(err) {
		fields = append(fields, issue.Field)
	}
	if !reflect.DeepEqual(fields, []string{"TITLE"}) {
		t.Fatalf("validation fields = %v; error = %v", fields, err)
	}
}

func TestMediaInspectorContinuesAfterMalformedM4ACredits(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "strict-import-aac.m4a"))
	if err != nil {
		t.Fatal(err)
	}
	titleOffset := bytes.Index(fixture, []byte("\xa9nam"))
	if titleOffset < 0 {
		t.Fatal("missing M4A title fixture atom")
	}
	copy(fixture[titleOffset:titleOffset+4], "----")
	// A malformed freeform child remains readable by ffprobe but not the credits parser.
	binary.BigEndian.PutUint32(fixture[titleOffset+4:titleOffset+8], 7)
	fixture = bytes.ReplaceAll(fixture, []byte("\xa9gen"), []byte("xxxx"))
	path := filepath.Join(t.TempDir(), "invalid-credits.m4a")
	if writeErr := os.WriteFile(path, fixture, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	_, err = library.NewMediaInspector().Inspect(context.Background(), path, nil)
	fields := []string{}
	for _, issue := range library.InspectionIssues(err) {
		fields = append(fields, issue.Field)
	}
	if !reflect.DeepEqual(fields, []string{"credits", "TITLE"}) {
		t.Fatalf("validation fields = %v; error = %v", fields, err)
	}
}

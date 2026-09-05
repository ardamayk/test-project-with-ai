package library

import (
	"context"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

// Every column of the tracks row, plus the source hash, reaches the API so a
// detail page can show everything the Music Server knows about a Track.
func TestGetTrackExposesEveryStoredColumn(t *testing.T) {
	db := testutil.OpenMigratedDB(t)
	_, trackID := seedTrack(t, db)
	if _, err := db.Exec(`UPDATE tracks SET musicbrainz_recording_id = 'rec-1', isrc = 'USUG12306672', acoustid_score = 0.97,
		metadata_source = 'musicbrainz', musicbrainz_changed_fields = '["title","artists"]', revision = 3 WHERE id = ?`, trackID); err != nil {
		t.Fatal(err)
	}

	track, err := NewStore(db).GetTrack(context.Background(), trackID)
	if err != nil {
		t.Fatalf("GetTrack() error = %v", err)
	}

	if track.MusicBrainzRecordingID != "rec-1" || track.ISRC != "USUG12306672" || track.AcoustIDScore == nil || *track.AcoustIDScore != 0.97 {
		t.Fatalf("identification fields = %+v", track)
	}
	if track.MetadataSource != "musicbrainz" || len(track.MusicBrainzChangedFields) != 2 || track.MusicBrainzChangedFields[0] != "title" {
		t.Fatalf("source = %q changed = %v", track.MetadataSource, track.MusicBrainzChangedFields)
	}
	if track.TitleSort == "" || track.Revision != 3 || track.FilePath == "" {
		t.Fatalf("stored columns missing: %+v", track)
	}
	if track.CreatedAt.IsZero() || track.UpdatedAt.IsZero() || track.FileMtime == 0 {
		t.Fatalf("timestamps missing: created %v updated %v mtime %d", track.CreatedAt, track.UpdatedAt, track.FileMtime)
	}
	if track.ContentSHA256 == "" {
		t.Fatalf("content hash missing: %+v", track)
	}
}

func TestGetTrackDefaultsIdentificationFieldsForTagOnlyTracks(t *testing.T) {
	db := testutil.OpenMigratedDB(t)
	_, trackID := seedTrack(t, db)

	track, err := NewStore(db).GetTrack(context.Background(), trackID)
	if err != nil {
		t.Fatal(err)
	}

	if track.MetadataSource != "file_tags" || track.MusicBrainzRecordingID != "" || track.AcoustIDScore != nil || len(track.MusicBrainzChangedFields) != 0 {
		t.Fatalf("tag-only track = %+v", track)
	}
}

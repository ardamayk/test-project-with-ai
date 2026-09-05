package managedimport

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/identification"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

type scriptedIdentifier struct {
	result identification.Identification
	paths  []string
	hints  []identification.Hint
}

func (identifier *scriptedIdentifier) Identify(_ context.Context, path string, hint identification.Hint) identification.Identification {
	identifier.paths = append(identifier.paths, path)
	identifier.hints = append(identifier.hints, hint)
	return identifier.result
}

func matchedRecording() identification.Identification {
	return identification.Identification{
		Outcome:  identification.OUTCOME_MATCHED,
		AcoustID: "acoustid-1",
		Score:    0.97,
		Recording: &identification.Recording{
			ID:      "5bcd7ba9-3b1f-4f1a-8a5a-8b0c9d1e2f30",
			Title:   "Welcome to New York (Taylor's Version)",
			Artists: []identification.Credit{{ID: "artist-mbid", Name: "Taylor Swift"}},
			ISRCs:   []string{"USUG12306672"},
			Genres:  []string{"pop"},
			Releases: []identification.Release{
				{ID: "rel-standard", Title: "1989 (Taylor's Version)", Status: "Official", Date: "2023-10-27", Year: 2023,
					ReleaseGroup: identification.ReleaseGroup{PrimaryType: "Album"}},
			},
		},
	}
}

// confirmBatchJob confirms one selected file of an Import Batch and returns
// the committed Track ID.
func confirmBatchJob(t *testing.T, service *Service, batchID, jobID string, decisions ...DuplicateDecision) string {
	t.Helper()
	ctx := context.Background()
	batch, err := service.store.GetBatch(ctx, batchID)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := service.ConfirmBatch(ctx, batchID, BatchConfirmation{Revision: batch.Revision, SelectedFileIDs: []string{jobID}, DuplicateDecisions: decisions})
	if err != nil {
		t.Fatalf("ConfirmBatch() error = %v", err)
	}
	for _, file := range completed.Files {
		if file.JobID == jobID {
			if file.Outcome != OUTCOME_IMPORTED || file.TrackID == "" {
				t.Fatalf("confirmed file = %+v", file)
			}
			return file.TrackID
		}
	}
	t.Fatalf("job %s missing from completed batch %+v", jobID, completed)
	return ""
}

func strictFLAC(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "library", "testdata", "strict-import.flac"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRecordingIdentificationMergesMusicBrainzRecordingFieldsAndPersistsThem(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	identifier := &scriptedIdentifier{result: matchedRecording()}
	module := NewModule(database, config.Config{ManagedStoragePath: t.TempDir()}, library.NewMediaInspector())
	module.service.identifier = identifier
	service := module.service
	ctx := context.Background()
	fixture := strictFLAC(t)

	batch, err := service.CreateBatch(ctx, BatchOptions{RecordingIdentification: true})
	if err != nil {
		t.Fatal(err)
	}
	job, err := service.CreateJob(ctx, batch.ID, "00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Upload(ctx, job.ID, "song.flac", bytes.NewReader(fixture), int64(len(fixture)))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if len(identifier.paths) != 1 {
		t.Fatalf("identifier called with %v, want the staged file once", identifier.paths)
	}
	if preview.Identification == nil {
		t.Fatalf("preview.Identification = nil")
	}
	if preview.Identification.Source != METADATA_SOURCE_MUSICBRAINZ || preview.Identification.Outcome != "matched" {
		t.Fatalf("identification = %+v", preview.Identification)
	}
	if preview.Identification.RecordingID != "5bcd7ba9-3b1f-4f1a-8a5a-8b0c9d1e2f30" || preview.Identification.AcoustIDScore != 0.97 {
		t.Fatalf("identification = %+v", preview.Identification)
	}
	if preview.File.Title != "Welcome to New York (Taylor's Version)" || !slices.Equal(preview.File.Artists, []string{"Taylor Swift"}) {
		t.Fatalf("merged file = %+v", preview.File)
	}
	if !slices.Contains(preview.Identification.ChangedFields, "title") {
		t.Fatalf("changedFields = %v, want title", preview.Identification.ChangedFields)
	}

	trackID := confirmBatchJob(t, service, batch.ID, job.ID)
	var title, artistName, source, recordingID, isrc, changed string
	var score float64
	err = database.QueryRow(`SELECT title, artist_name, metadata_source, musicbrainz_recording_id, isrc, acoustid_score, musicbrainz_changed_fields FROM tracks WHERE id = ?`, trackID).
		Scan(&title, &artistName, &source, &recordingID, &isrc, &score, &changed)
	if err != nil {
		t.Fatalf("read committed Track: %v", err)
	}
	if title != "Welcome to New York (Taylor's Version)" || artistName != "Taylor Swift" {
		t.Fatalf("committed title = %q artist = %q", title, artistName)
	}
	if source != "musicbrainz" || recordingID != "5bcd7ba9-3b1f-4f1a-8a5a-8b0c9d1e2f30" || isrc != "USUG12306672" || score != 0.97 {
		t.Fatalf("committed identification = %s %s %s %v", source, recordingID, isrc, score)
	}
	if changed == "[]" {
		t.Fatalf("committed changed fields = %s", changed)
	}
}

func TestRecordingIdentificationSkipsFingerprintingWhenBatchSwitchIsOff(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	identifier := &scriptedIdentifier{result: matchedRecording()}
	module := NewModule(database, config.Config{ManagedStoragePath: t.TempDir()}, library.NewMediaInspector())
	module.service.identifier = identifier
	service := module.service
	ctx := context.Background()
	fixture := strictFLAC(t)

	batch, err := service.CreateBatch(ctx, BatchOptions{RecordingIdentification: false})
	if err != nil {
		t.Fatal(err)
	}
	job, err := service.CreateJob(ctx, batch.ID, "00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Upload(ctx, job.ID, "song.flac", bytes.NewReader(fixture), int64(len(fixture)))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if len(identifier.paths) != 0 {
		t.Fatalf("identifier must not run when the batch switch is off, got %v", identifier.paths)
	}
	if preview.Identification == nil || preview.Identification.Source != METADATA_SOURCE_FILE_TAGS || preview.Identification.Reason == "" {
		t.Fatalf("identification = %+v", preview.Identification)
	}
	trackID := confirmBatchJob(t, service, batch.ID, job.ID)
	var source string
	var recordingID *string
	if err := database.QueryRow(`SELECT metadata_source, musicbrainz_recording_id FROM tracks WHERE id = ?`, trackID).Scan(&source, &recordingID); err != nil {
		t.Fatal(err)
	}
	if source != "file_tags" || recordingID != nil {
		t.Fatalf("committed source = %q recording = %v", source, recordingID)
	}
}

func TestRecordingIdentificationFallsBackToTagsWhenServicesAreUnavailable(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	identifier := &scriptedIdentifier{result: identification.Identification{Outcome: identification.OUTCOME_UNAVAILABLE, Reason: "AcoustID returned HTTP 503"}}
	module := NewModule(database, config.Config{ManagedStoragePath: t.TempDir()}, library.NewMediaInspector())
	module.service.identifier = identifier
	service := module.service
	ctx := context.Background()
	fixture := strictFLAC(t)

	batch, _ := service.CreateBatch(ctx, BatchOptions{RecordingIdentification: true})
	job, _ := service.CreateJob(ctx, batch.ID, "00000000-0000-4000-8000-000000000001")
	preview, err := service.Upload(ctx, job.ID, "song.flac", bytes.NewReader(fixture), int64(len(fixture)))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if preview.Identification.Source != METADATA_SOURCE_FILE_TAGS || preview.Identification.Outcome != "unavailable" || preview.Identification.Reason != "AcoustID returned HTTP 503" {
		t.Fatalf("identification = %+v", preview.Identification)
	}
	if preview.File.Title == "Welcome to New York (Taylor's Version)" {
		t.Fatalf("tags must stay untouched without a match")
	}
}

func TestRecordingIdentificationFlagsSameRecordingAsRecordingDuplicate(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	identifier := &scriptedIdentifier{result: matchedRecording()}
	module := NewModule(database, config.Config{ManagedStoragePath: t.TempDir()}, library.NewMediaInspector())
	module.service.identifier = identifier
	service := module.service
	ctx := context.Background()

	batch, _ := service.CreateBatch(ctx, BatchOptions{RecordingIdentification: true})
	first, _ := service.CreateJob(ctx, batch.ID, "00000000-0000-4000-8000-000000000001")
	flac := strictFLAC(t)
	if _, err := service.Upload(ctx, first.ID, "song.flac", bytes.NewReader(flac), int64(len(flac))); err != nil {
		t.Fatal(err)
	}
	confirmBatchJob(t, service, batch.ID, first.ID)

	secondBatch, _ := service.CreateBatch(ctx, BatchOptions{RecordingIdentification: true})
	second, _ := service.CreateJob(ctx, secondBatch.ID, "00000000-0000-4000-8000-000000000002")
	mp3 := testutil.StrictMP3Fixture()
	secondPreview, err := service.Upload(ctx, second.ID, "song.mp3", bytes.NewReader(mp3), int64(len(mp3)))
	if err != nil {
		t.Fatalf("second Upload() error = %v", err)
	}

	if secondPreview.DuplicateClassification != DUPLICATE_RECORDING {
		t.Fatalf("classification = %s, want recording_duplicate", secondPreview.DuplicateClassification)
	}
	if len(secondPreview.DuplicateCandidates) != 1 || secondPreview.DuplicateCandidates[0].Title != "Welcome to New York (Taylor's Version)" {
		t.Fatalf("candidates = %+v", secondPreview.DuplicateCandidates)
	}
	reloaded, err := service.store.GetBatch(ctx, secondBatch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmBatch(ctx, secondBatch.ID, BatchConfirmation{Revision: reloaded.Revision, SelectedFileIDs: []string{second.ID}}); err == nil {
		t.Fatalf("ConfirmBatch() without a decision must fail for a Recording Duplicate")
	}
	trackID := confirmBatchJob(t, service, secondBatch.ID, second.ID, DuplicateDecision{JobID: second.ID, Action: DUPLICATE_ACTION_IMPORT_SEPARATELY})
	var source, recordingID string
	if err := database.QueryRow(`SELECT metadata_source, musicbrainz_recording_id FROM tracks WHERE id = ?`, trackID).Scan(&source, &recordingID); err != nil {
		t.Fatal(err)
	}
	if source != "musicbrainz" || recordingID != "5bcd7ba9-3b1f-4f1a-8a5a-8b0c9d1e2f30" {
		t.Fatalf("second Track source = %q recording = %q", source, recordingID)
	}
}

func TestRecordingIdentificationAppearsInImportHistory(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	module := NewModule(database, config.Config{ManagedStoragePath: t.TempDir()}, library.NewMediaInspector())
	module.service.identifier = &scriptedIdentifier{result: matchedRecording()}
	service := module.service
	ctx := context.Background()

	batch, _ := service.CreateBatch(ctx, BatchOptions{RecordingIdentification: true})
	job, _ := service.CreateJob(ctx, batch.ID, "00000000-0000-4000-8000-000000000001")
	flac := strictFLAC(t)
	if _, err := service.Upload(ctx, job.ID, "song.flac", bytes.NewReader(flac), int64(len(flac))); err != nil {
		t.Fatal(err)
	}
	confirmBatchJob(t, service, batch.ID, job.ID)

	history, err := service.ListHistory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Items) != 1 || len(history.Items[0].Files) != 1 {
		t.Fatalf("history = %+v", history)
	}
	file := history.Items[0].Files[0]
	if file.MetadataSource != METADATA_SOURCE_MUSICBRAINZ || file.AcoustIDScore != 0.97 || file.RecordingID != "5bcd7ba9-3b1f-4f1a-8a5a-8b0c9d1e2f30" {
		t.Fatalf("history file = %+v", file)
	}
}

func TestRecordingIdentificationCarriesIntoTrackReplacement(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	module := NewModule(database, config.Config{ManagedStoragePath: t.TempDir()}, library.NewMediaInspector())
	identifier := &scriptedIdentifier{result: identification.Identification{Outcome: identification.OUTCOME_NO_MATCH, Reason: "nothing yet"}}
	module.service.identifier = identifier
	service := module.service
	ctx := context.Background()

	batch, _ := service.CreateBatch(ctx, BatchOptions{RecordingIdentification: true})
	job, _ := service.CreateJob(ctx, batch.ID, "00000000-0000-4000-8000-000000000001")
	flac := strictFLAC(t)
	if _, err := service.Upload(ctx, job.ID, "song.flac", bytes.NewReader(flac), int64(len(flac))); err != nil {
		t.Fatal(err)
	}
	trackID := confirmBatchJob(t, service, batch.ID, job.ID)

	identifier.result = matchedRecording()
	replacementJob, err := service.CreateReplacementJob(ctx, trackID)
	if err != nil {
		t.Fatal(err)
	}
	mp3 := testutil.StrictMP3Fixture()
	preview, err := service.Upload(ctx, replacementJob.ID, "song.mp3", bytes.NewReader(mp3), int64(len(mp3)))
	if err != nil {
		t.Fatalf("replacement Upload() error = %v", err)
	}
	if preview.Replacement == nil || preview.File.Title != "Welcome to New York (Taylor's Version)" {
		t.Fatalf("replacement preview = %+v", preview)
	}
	if _, err := service.ConfirmReplacement(ctx, replacementJob.ID, TrackReplacementConfirmation{Revision: preview.Revision, ConfirmationToken: preview.Replacement.ConfirmationToken}); err != nil {
		t.Fatalf("ConfirmReplacement() error = %v", err)
	}

	var title, source, recordingID string
	if err := database.QueryRow(`SELECT title, metadata_source, musicbrainz_recording_id FROM tracks WHERE id = ?`, trackID).Scan(&title, &source, &recordingID); err != nil {
		t.Fatal(err)
	}
	if title != "Welcome to New York (Taylor's Version)" || source != "musicbrainz" || recordingID != "5bcd7ba9-3b1f-4f1a-8a5a-8b0c9d1e2f30" {
		t.Fatalf("replaced Track = %q %q %q", title, source, recordingID)
	}
}

func taggedMP3() []byte {
	return testutil.StrictMP3FixtureWithExtraFrames(4,
		testutil.ID3TextFrame(4, "TSRC", "USUG12306672"),
		testutil.ID3UFIDFrame(4, "http://musicbrainz.org", "2872b086-7c8d-4b3a-9b91-55805cd912b0"),
	)
}

func TestTaggedIdentifiersArePersistedWithoutRecordingIdentification(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	module := NewModule(database, config.Config{ManagedStoragePath: t.TempDir()}, library.NewMediaInspector())
	module.service.identifier = nil
	service := module.service
	ctx := context.Background()

	batch, _ := service.CreateBatch(ctx, BatchOptions{RecordingIdentification: true})
	job, _ := service.CreateJob(ctx, batch.ID, "00000000-0000-4000-8000-000000000001")
	mp3 := taggedMP3()
	preview, err := service.Upload(ctx, job.ID, "tagged.mp3", bytes.NewReader(mp3), int64(len(mp3)))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if preview.Identification.Source != METADATA_SOURCE_FILE_TAGS || preview.Identification.RecordingID != "2872b086-7c8d-4b3a-9b91-55805cd912b0" || preview.Identification.ISRC != "USUG12306672" {
		t.Fatalf("identification = %+v", preview.Identification)
	}
	trackID := confirmBatchJob(t, service, batch.ID, job.ID)

	var source, recordingID, isrc string
	if err := database.QueryRow(`SELECT metadata_source, musicbrainz_recording_id, isrc FROM tracks WHERE id = ?`, trackID).Scan(&source, &recordingID, &isrc); err != nil {
		t.Fatal(err)
	}
	if source != "file_tags" || recordingID != "2872b086-7c8d-4b3a-9b91-55805cd912b0" || isrc != "USUG12306672" {
		t.Fatalf("track = %s %s %s", source, recordingID, isrc)
	}
}

func TestTaggedIdentifiersAreHandedToTheIdentifier(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	identifier := &scriptedIdentifier{result: matchedRecording()}
	module := NewModule(database, config.Config{ManagedStoragePath: t.TempDir()}, library.NewMediaInspector())
	module.service.identifier = identifier
	service := module.service
	ctx := context.Background()

	batch, _ := service.CreateBatch(ctx, BatchOptions{RecordingIdentification: true})
	job, _ := service.CreateJob(ctx, batch.ID, "00000000-0000-4000-8000-000000000001")
	mp3 := taggedMP3()
	if _, err := service.Upload(ctx, job.ID, "tagged.mp3", bytes.NewReader(mp3), int64(len(mp3))); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if len(identifier.hints) != 1 || identifier.hints[0].RecordingID != "2872b086-7c8d-4b3a-9b91-55805cd912b0" || identifier.hints[0].ISRC != "USUG12306672" {
		t.Fatalf("hints = %+v", identifier.hints)
	}
}

func TestTaggedRecordingIDFlagsRecordingDuplicateOffline(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	module := NewModule(database, config.Config{ManagedStoragePath: t.TempDir()}, library.NewMediaInspector())
	module.service.identifier = nil
	service := module.service
	ctx := context.Background()

	batch, _ := service.CreateBatch(ctx, BatchOptions{})
	first, _ := service.CreateJob(ctx, batch.ID, "00000000-0000-4000-8000-000000000001")
	mp3 := taggedMP3()
	if _, err := service.Upload(ctx, first.ID, "tagged.mp3", bytes.NewReader(mp3), int64(len(mp3))); err != nil {
		t.Fatal(err)
	}
	confirmBatchJob(t, service, batch.ID, first.ID)

	flacWithSameTag := strictFLAC(t)
	_ = flacWithSameTag
	other := testutil.StrictMP3FixtureWithExtraFrames(3,
		testutil.ID3UFIDFrame(3, "http://musicbrainz.org", "2872b086-7c8d-4b3a-9b91-55805cd912b0"),
	)
	secondBatch, _ := service.CreateBatch(ctx, BatchOptions{})
	second, _ := service.CreateJob(ctx, secondBatch.ID, "00000000-0000-4000-8000-000000000003")
	preview, err := service.Upload(ctx, second.ID, "other.mp3", bytes.NewReader(other), int64(len(other)))
	if err != nil {
		t.Fatalf("second Upload() error = %v", err)
	}
	if preview.DuplicateClassification != DUPLICATE_RECORDING {
		t.Fatalf("classification = %s, want recording_duplicate from tagged MBIDs alone", preview.DuplicateClassification)
	}
}

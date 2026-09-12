package managedimport_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/modules/managedimport"
	"github.com/ardam/navidrome-replacement/server/internal/modules/playback"
	"github.com/ardam/navidrome-replacement/server/internal/modules/playlists"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

func TestArtistCreditRepairPreviewDoesNotWrite(t *testing.T) {
	database, root, databasePath, trackID, _ := creditRepairFixture(t)
	before := listTracks(t, newManagedImportTestRouterWithDatabase(t, database, root))
	report, err := managedimport.PreviewArtistCreditRepair(context.Background(), databasePath, root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Applied || len(report.Changes) != 2 {
		t.Fatalf("preview = %+v", report)
	}
	after := listTracks(t, newManagedImportTestRouterWithDatabase(t, database, root))
	if len(after.Items) != 1 || after.Items[0].ID != trackID || after.Items[0].Artists[0].Name != before.Items[0].Artists[0].Name {
		t.Fatalf("preview wrote metadata: %+v", after)
	}
}

func TestArtistCreditRepairApplyPreservesLibraryAndIsIdempotent(t *testing.T) {
	database, root, databasePath, trackID, albumID := creditRepairFixture(t)
	router := newManagedImportTestRouterWithDatabase(t, database, root)
	before := listTracks(t, router).Items[0]
	seedDeletionReferences(t, database, trackID)
	repairExec(t, database, "UPDATE playlists SET user_id='00000000-0000-0000-0000-000000000001'; UPDATE playback_queue SET user_id='00000000-0000-0000-0000-000000000001'")
	referenceRouter := chi.NewRouter()
	access := library.NewModule(database).TrackAccess()
	playback.NewModule(database, access).RegisterRoutes(referenceRouter)
	playlists.NewModule(database, access).RegisterRoutes(referenceRouter)
	readReferences := func() []any {
		results := []any{}
		for _, path := range []string{"/api/v1/playback/queue", "/api/v1/playlists/playlist-1"} {
			response := testutil.ServeRequest(t, referenceRouter, http.MethodGet, path, nil, nil)
			if response.Code != http.StatusOK {
				t.Fatalf("reference read: %s", response.Body.String())
			}
			var value any
			testutil.DecodeJSON(t, response, &value)
			// Compare identity/reference fields; nested Track credits intentionally change.
			var strip func(any)
			strip = func(value any) {
				switch typed := value.(type) {
				case map[string]any:
					delete(typed, "artists")
					delete(typed, "albumArtists")
					delete(typed, "artistName")
					delete(typed, "revision")
					delete(typed, "updatedAt")
					for _, child := range typed {
						strip(child)
					}
				case []any:
					for _, child := range typed {
						strip(child)
					}
				}
			}
			strip(value)
			results = append(results, value)
		}
		return results
	}
	referencesBefore := readReferences()
	var sourcePath string
	if err := database.QueryRow("SELECT file_path FROM track_sources WHERE track_id = ?", trackID).Scan(&sourcePath); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	report, err := managedimport.PreviewArtistCreditRepair(context.Background(), databasePath, root)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup.sqlite")
	if err := report.Apply(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
	if !report.Applied || report.BackupPath != backup {
		t.Fatalf("apply report = %+v", report)
	}
	after := listTracks(t, router).Items[0]
	if after.ID != trackID || after.AlbumID != albumID || after.Title != before.Title || after.DurationMs != before.DurationMs || after.Artists[0].Name != "Test Artist" {
		t.Fatalf("repaired Track = %+v", after)
	}
	assertNormalizedAlbum(t, router, albumID, trackID)
	if got := readReferences(); !reflect.DeepEqual(got, referencesBefore) {
		t.Fatalf("Playlist/Queue changed: before=%+v after=%+v", referencesBefore, got)
	}
	current, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, current) {
		t.Fatal("audio bytes changed")
	}
	backupReport, err := managedimport.PreviewArtistCreditRepair(context.Background(), backup, root)
	if err != nil || len(backupReport.Changes) != 2 {
		t.Fatalf("backup does not contain original metadata: %+v, %v", backupReport, err)
	}
	again, err := managedimport.PreviewArtistCreditRepair(context.Background(), databasePath, root)
	if err != nil || len(again.Changes) != 0 {
		t.Fatalf("second repair = %+v, %v", again, err)
	}
}

func creditRepairFixture(t *testing.T) (*sql.DB, string, string, string, string) {
	t.Helper()
	database := testutil.OpenMigratedDB(t)
	var sequence int
	var schema, databasePath string
	if err := database.QueryRow("PRAGMA database_list").Scan(&sequence, &schema, &databasePath); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	router := newManagedImportTestRouterWithDatabase(t, database, root)
	jobID, revision := uploadFLACForPreview(t, router, readStrictFLACFixture(t), "credits.flac")
	response := serveImportConfirmation(router, jobID, revision)
	if response.Code != http.StatusOK {
		t.Fatalf("import: %s", response.Body.String())
	}
	tracks := listTracks(t, router)
	trackID, albumID := tracks.Items[0].ID, tracks.Items[0].AlbumID
	// Reproduce old combined credits without changing the authoritative file.
	repairExec(t, database, "UPDATE artists SET name = 'Old combined credit', name_normalized = 'old combined credit' WHERE id IN (SELECT artist_id FROM track_artists WHERE track_id = ?)", trackID)
	repairExec(t, database, "UPDATE tracks SET artist_name = 'Old combined credit' WHERE id = ?", trackID)
	repairExec(t, database, "UPDATE artists SET name = 'Old album credit', name_normalized = 'old album credit' WHERE id IN (SELECT artist_id FROM album_artists WHERE album_id = ?)", albumID)
	repairExec(t, database, "UPDATE albums SET identity_key = ? WHERE id = ?", "old album credit\x1fstrict import tests", albumID)
	return database, root, databasePath, trackID, albumID
}

func repairExec(t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestArtistCreditRepairRejectsUnsafeApplyWithoutWriting(t *testing.T) {
	for _, scenario := range []string{"existing backup", "empty backup", "stale revision", "stale source revision", "stale path", "stale visibility", "stale membership", "changed bytes", "pending journal", "SQL rollback"} {
		t.Run(scenario, func(t *testing.T) {
			database, root, path, trackID, albumID := creditRepairFixture(t)
			router := newManagedImportTestRouterWithDatabase(t, database, root)
			report, err := managedimport.PreviewArtistCreditRepair(context.Background(), path, root)
			if err != nil {
				t.Fatal(err)
			}
			backup := filepath.Join(t.TempDir(), "backup.sqlite")
			switch scenario {
			case "existing backup":
				if err := os.WriteFile(backup, []byte("keep me"), 0600); err != nil {
					t.Fatal(err)
				}
			case "empty backup":
				backup = ""
			case "stale revision":
				repairExec(t, database, "UPDATE tracks SET revision=revision+1 WHERE id=?", trackID)
			case "stale source revision":
				repairExec(t, database, "UPDATE track_sources SET revision=revision+1 WHERE track_id=?", trackID)
			case "stale path":
				repairExec(t, database, "UPDATE track_sources SET file_path=file_path || '.moved' WHERE track_id=?", trackID)
			case "stale visibility":
				repairExec(t, database, "UPDATE tracks SET missing_at=CURRENT_TIMESTAMP WHERE id=?", trackID)
			case "stale membership":
				testutil.SeedManagedTrack(t, database, testutil.ManagedTrackSpec{AlbumID: albumID, Artist: "Old combined credit", AlbumArtist: "Old album credit", Title: "New member"})
			case "changed bytes":
				var source string
				if err := database.QueryRow("SELECT file_path FROM track_sources WHERE track_id=?", trackID).Scan(&source); err != nil {
					t.Fatal(err)
				}
				bytes, err := os.ReadFile(source)
				if err != nil {
					t.Fatal(err)
				}
				bytes[len(bytes)-1] ^= 1
				if err := os.WriteFile(source, bytes, 0600); err != nil {
					t.Fatal(err)
				}
			case "pending journal":
				repairExec(t, database, "INSERT INTO permanent_track_deletions(track_id,file_path,content_sha256) SELECT track_id,file_path,content_sha256 FROM track_sources WHERE track_id=?", trackID)
			case "SQL rollback":
				repairExec(t, database, "CREATE TRIGGER fail_credit_repair BEFORE UPDATE OF artist_id ON albums BEGIN SELECT RAISE(ABORT,'fixture write failure'); END")
			}
			if err := report.Apply(context.Background(), backup); err == nil {
				t.Fatal("unsafe repair succeeded")
			}
			if report.Applied {
				t.Fatal("failed repair marked applied")
			}
			if scenario == "stale visibility" {
				repairExec(t, database, "UPDATE tracks SET missing_at=NULL WHERE id=?", trackID)
			}
			tracks := listTracks(t, router)
			for _, track := range tracks.Items {
				if track.ID == trackID && track.Artists[0].Name != "Old combined credit" {
					t.Fatalf("failed repair changed credits: %+v", track)
				}
			}
			if scenario == "existing backup" {
				bytes, err := os.ReadFile(backup)
				if err != nil || string(bytes) != "keep me" {
					t.Fatalf("backup overwritten: %q %v", bytes, err)
				}
			}
		})
	}
}

func TestArtistCreditRepairAlbumConflictsLeaveTrackRepairIndependent(t *testing.T) {
	for _, scenario := range []string{"identity collision", "unknown identity", "invalid edition", "unread member", "disagreeing members"} {
		t.Run(scenario, func(t *testing.T) {
			database, root, path, trackID, albumID := creditRepairFixture(t)
			router := newManagedImportTestRouterWithDatabase(t, database, root)
			switch scenario {
			case "identity collision":
				importOneFLAC(t, router, secondTrackFixture(readStrictFLACFixture(t)), "other.flac")
			case "unknown identity":
				repairExec(t, database, "UPDATE albums SET identity_key='unrecognized' WHERE id=?", albumID)
			case "invalid edition":
				repairExec(t, database, "UPDATE albums SET identity_key=identity_key || ? WHERE id=?", "\x1fmanaged-import-edition:not-an-edition", albumID)
			case "unread member":
				testutil.SeedManagedTrack(t, database, testutil.ManagedTrackSpec{AlbumID: albumID, Artist: "Old combined credit", AlbumArtist: "Old album credit"})
			case "disagreeing members":
				fixture := replaceFixtureTag(t, secondTrackFixture(readStrictFLACFixture(t)), "ALBUMARTIST=Test Album Artist", "ALBUMARTIST=Other Album")
				other := importOneFLAC(t, router, withoutFrontCover(t, fixture), "disagree.flac")
				repairExec(t, database, "UPDATE tracks SET album_id=? WHERE id=?", albumID, other)
			}
			report, err := managedimport.PreviewArtistCreditRepair(context.Background(), path, root)
			if err != nil {
				t.Fatal(err)
			}
			for _, change := range report.Changes {
				if change.Kind == "album" && change.ID == albumID {
					t.Fatalf("unsafe Album change: %+v", change)
				}
			}
			if len(report.Skipped)+len(report.Conflicts) == 0 {
				t.Fatalf("missing conflict report: %+v", report)
			}
			if err := report.Apply(context.Background(), filepath.Join(t.TempDir(), "backup.sqlite")); err != nil {
				t.Fatal(err)
			}
			for _, track := range listTracks(t, router).Items {
				if track.ID == trackID && track.Artists[0].Name != "Test Artist" {
					t.Fatalf("safe Track repair blocked: %+v", track)
				}
			}
			response := testutil.ServeRequest(t, router, http.MethodGet, "/api/v1/library/albums/"+albumID, nil, nil)
			var album struct {
				AlbumArtists []struct{ Name string } `json:"albumArtists"`
			}
			testutil.DecodeJSON(t, response, &album)
			if len(album.AlbumArtists) != 1 || album.AlbumArtists[0].Name != "Old album credit" {
				t.Fatalf("conflicting Album changed: %+v", album)
			}
		})
	}
}

func TestArtistCreditRepairRestoresOrderedCredits(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	root := t.TempDir()
	path := filepath.Join(root, "managed.wav")
	fixture := testutil.BuildWAV(testutil.WAVFixture{AudioFormat: 1, ChannelCount: 2, SampleRateHz: 44100, BitDepth: 16, PCMFrames: 4410, ID3Frames: []testutil.WAVID3Frame{
		testutil.WAVTextFrame("TIT2", "Repair song"), testutil.WAVTextFrame("TALB", "Repair album"),
		testutil.WAVTextFrame("TPE1", "Second Artist\x00Earth, Wind & Fire"), testutil.WAVTextFrame("TPE2", "Album Second\x00Album First"), testutil.WAVTextFrame("TRCK", "1"),
	}})
	if err := os.WriteFile(path, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	albumID, trackID := testutil.SeedManagedTrack(t, database, testutil.ManagedTrackSpec{Title: "Repair song", Album: "Repair album", Artist: "Old combined", AlbumArtist: "Old album", FilePath: path, SizeBytes: int64(len(fixture)), Format: "wav", TrackNo: 1})
	repairExec(t, database, "UPDATE track_sources SET content_sha256=? WHERE track_id=?", fmt.Sprintf("%x", sha256.Sum256(fixture)), trackID)
	repairExec(t, database, "UPDATE albums SET identity_key=? WHERE id=?", "old album\x1frepair album", albumID)
	var seq int
	var schema, databasePath string
	if err := database.QueryRow("PRAGMA database_list").Scan(&seq, &schema, &databasePath); err != nil {
		t.Fatal(err)
	}
	report, err := managedimport.PreviewArtistCreditRepair(context.Background(), databasePath, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Apply(context.Background(), filepath.Join(t.TempDir(), "backup.sqlite")); err != nil {
		t.Fatal(err)
	}
	router := newManagedImportTestRouterWithDatabase(t, database, root)
	track := listTracks(t, router).Items[0]
	if len(track.Artists) != 2 || track.Artists[0].Name != "Second Artist" || track.Artists[1].Name != "Earth, Wind & Fire" {
		t.Fatalf("ordered Track credits = %+v", track.Artists)
	}
	response := testutil.ServeRequest(t, router, http.MethodGet, "/api/v1/library/albums/"+albumID, nil, nil)
	var album struct {
		AlbumArtists []struct{ Name string } `json:"albumArtists"`
	}
	testutil.DecodeJSON(t, response, &album)
	if len(album.AlbumArtists) != 2 || album.AlbumArtists[0].Name != "Album Second" || album.AlbumArtists[1].Name != "Album First" {
		t.Fatalf("ordered Album credits = %+v", album)
	}
	again, err := managedimport.PreviewArtistCreditRepair(context.Background(), databasePath, root)
	if err != nil || len(again.Changes) != 0 {
		t.Fatalf("ordered repair not idempotent: %+v %v", again, err)
	}
}

func TestArtistCreditRepairRequiresVerifiedBackup(t *testing.T) {
	database, root, path, trackID, _ := creditRepairFixture(t)
	// A readable SQLite file with broken references must not pass backup verification.
	repairExec(t, database, "PRAGMA foreign_keys=OFF; INSERT INTO playlist_tracks(playlist_id,track_id,position) VALUES ('missing-playlist',?,0); PRAGMA foreign_keys=ON", trackID)
	report, err := managedimport.PreviewArtistCreditRepair(context.Background(), path, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Apply(context.Background(), filepath.Join(t.TempDir(), "backup.sqlite")); err == nil || !strings.Contains(err.Error(), "backup foreign key check failed") {
		t.Fatalf("unverified backup accepted: %v", err)
	}
	if got := listTracks(t, newManagedImportTestRouterWithDatabase(t, database, root)).Items[0].Artists[0].Name; got != "Old combined credit" {
		t.Fatalf("backup failure wrote %q", got)
	}
}

func TestArtistCreditRepairPreservesEditionIdentity(t *testing.T) {
	database, root, path, _, albumID := creditRepairFixture(t)
	const edition = "\x1fmanaged-import-edition:c2f0eeef-a8de-4efb-af7b-f6dc2fcad4a5"
	repairExec(t, database, "UPDATE albums SET identity_key=identity_key || ? WHERE id=?", edition, albumID)
	report, err := managedimport.PreviewArtistCreditRepair(context.Background(), path, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Apply(context.Background(), filepath.Join(t.TempDir(), "backup.sqlite")); err != nil {
		t.Fatal(err)
	}
	// A normal import remains a distinct Album: the repaired edition never loses its suffix.
	router := newManagedImportTestRouterWithDatabase(t, database, root)
	other := importOneFLAC(t, router, secondTrackFixture(readStrictFLACFixture(t)), "normal-edition.flac")
	for _, track := range listTracks(t, router).Items {
		if track.ID == other && track.AlbumID == albumID {
			t.Fatal("repair collapsed the separate edition")
		}
	}
	again, err := managedimport.PreviewArtistCreditRepair(context.Background(), path, root)
	if err != nil || len(again.Changes) != 0 {
		t.Fatalf("edition repair not idempotent: %+v %v", again, err)
	}
}

func TestArtistCreditRepairSkipsInvalidSources(t *testing.T) {
	for _, scenario := range []string{"missing", "changed hash", "size", "outside root", "symlink", "directory", "invalid metadata", "hidden"} {
		t.Run(scenario, func(t *testing.T) {
			database, root, path, trackID, _ := creditRepairFixture(t)
			var source string
			if err := database.QueryRow("SELECT file_path FROM track_sources WHERE track_id=?", trackID).Scan(&source); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "missing":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
			case "changed hash":
				if err := os.WriteFile(source, []byte("different bytes"), 0600); err != nil {
					t.Fatal(err)
				}
			case "size":
				repairExec(t, database, "UPDATE track_sources SET size_bytes=size_bytes+1 WHERE track_id=?", trackID)
			case "outside root":
				outside := filepath.Join(t.TempDir(), "outside.flac")
				if err := os.Rename(source, outside); err != nil {
					t.Fatal(err)
				}
				repairExec(t, database, "UPDATE track_sources SET file_path=? WHERE track_id=?", outside, trackID)
				repairExec(t, database, "UPDATE tracks SET file_path=? WHERE id=?", outside, trackID)
			case "symlink":
				target := filepath.Join(t.TempDir(), "target.flac")
				if err := os.Rename(source, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, source); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(source, 0700); err != nil {
					t.Fatal(err)
				}
			case "invalid metadata":
				invalid := replaceFixtureTag(t, readStrictFLACFixture(t), "ALBUMARTIST=Test Album Artist", "")
				if err := os.WriteFile(source, invalid, 0600); err != nil {
					t.Fatal(err)
				}
				repairExec(t, database, "UPDATE track_sources SET content_sha256=? WHERE track_id=?", fmt.Sprintf("%x", sha256.Sum256(invalid)), trackID)
			case "hidden":
				repairExec(t, database, "UPDATE tracks SET is_pending_commit=1 WHERE id=?", trackID)
			}
			report, err := managedimport.PreviewArtistCreditRepair(context.Background(), path, root)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Changes) != 0 || len(report.Skipped) != 2 {
				t.Fatalf("unsafe source accepted: %+v", report)
			}
			if scenario == "invalid metadata" && !strings.Contains(report.Skipped[0].Reason, "ALBUMARTIST") {
				t.Fatalf("inspector validation missing: %+v", report)
			}
		})
	}
}

func TestArtistCreditRepairCommandDefaultsToReport(t *testing.T) {
	database, root, path, _, _ := creditRepairFixture(t)
	binary := filepath.Join(t.TempDir(), "repair-artist-credits")
	build := exec.Command("go", "build", "-o", binary, "../../../cmd/repair-artist-credits")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build repair command: %v\n%s", err, output)
	}
	output, err := exec.Command(binary, "--db", path, "--managed-root", root).Output()
	if err != nil {
		t.Fatal(err)
	}
	var report managedimport.ArtistCreditRepairReport
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatal(err)
	}
	if report.Applied || len(report.Changes) != 2 {
		t.Fatalf("default command report = %s", output)
	}
	if got := listTracks(t, newManagedImportTestRouterWithDatabase(t, database, root)).Items[0].Artists[0].Name; got != "Old combined credit" {
		t.Fatalf("default command wrote %q", got)
	}
	if output, err := exec.Command(binary, "--db", path, "--managed-root", root, "--apply").CombinedOutput(); err == nil {
		t.Fatalf("apply without backup accepted: %s", output)
	}
	backup := filepath.Join(t.TempDir(), "backup.sqlite")
	if output, err := exec.Command(binary, "--db", path, "--managed-root", root, "--apply", "--backup", backup).CombinedOutput(); err != nil {
		t.Fatalf("command apply: %v\n%s", err, output)
	}
	if got := listTracks(t, newManagedImportTestRouterWithDatabase(t, database, root)).Items[0].Artists[0].Name; got != "Test Artist" {
		t.Fatalf("apply credit = %q", got)
	}
}

func TestArtistCreditRepairMissingRootDoesNotCreateDirectories(t *testing.T) {
	_, _, path, _, _ := creditRepairFixture(t)
	root := filepath.Join(t.TempDir(), "missing-root")
	report, err := managedimport.PreviewArtistCreditRepair(context.Background(), path, root)
	if err == nil {
		t.Fatal("missing root accepted")
	}
	if err := report.Apply(context.Background(), filepath.Join(t.TempDir(), "backup.sqlite")); err == nil {
		t.Fatal("failed preview accepted for apply")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("preview created root: %v", err)
	}
}

func TestArtistCreditRepairMissingDatabaseDoesNotCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sqlite")
	if _, err := managedimport.PreviewArtistCreditRepair(context.Background(), path, t.TempDir()); err == nil {
		t.Fatal("missing database accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("database created: %v", err)
	}
}

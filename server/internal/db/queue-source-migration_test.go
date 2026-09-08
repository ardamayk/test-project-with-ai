package db_test

import (
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/pressly/goose/v3"
)

func TestQueueSourceMigrationDefaultsLegacyRowsAndRollsBack(t *testing.T) {
	const previousVersion = 34
	const sourceVersion = 35
	database := openDatabaseAtVersion(t, previousVersion)
	_, trackID := testutil.SeedManagedTrack(t, database, testutil.ManagedTrackSpec{})
	if _, err := database.Exec(`INSERT INTO playback_queue (id, user_id, position, track_id) VALUES ('legacy', 'user-1', 0, ?)`, trackID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO playback_queue_state (user_id, revision, event_sequence) VALUES ('user-1', 7, 9)`); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database, migrationsDir(t), sourceVersion); err != nil {
		t.Fatal(err)
	}
	var source string
	if err := database.QueryRow(`SELECT source FROM playback_queue WHERE id = 'legacy'`).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != `{"kind":"user"}` {
		t.Fatalf("legacy source = %s", source)
	}
	var revision, sequence int
	if err := database.QueryRow(`SELECT revision, event_sequence FROM playback_queue_state WHERE user_id = 'user-1'`).Scan(&revision, &sequence); err != nil {
		t.Fatal(err)
	}
	if revision != 7 || sequence != 9 {
		t.Fatalf("migration changed revision/sequence: %d/%d", revision, sequence)
	}
	if err := goose.Down(database, migrationsDir(t)); err != nil {
		t.Fatal(err)
	}
	assertMigrationVersion(t, database, previousVersion)
	var remainingTrackID string
	if err := database.QueryRow(`SELECT track_id FROM playback_queue WHERE id = 'legacy'`).Scan(&remainingTrackID); err != nil {
		t.Fatal(err)
	}
	if remainingTrackID != trackID {
		t.Fatalf("rollback changed legacy track: %s", remainingTrackID)
	}
}

package identification_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/identification"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

func TestCachedAcoustIDLooksUpOnceForTheSameFingerprint(t *testing.T) {
	sqlDB := testutil.OpenMigratedDB(t)
	upstream := &fakeAcoustID{results: []identification.AcoustIDResult{{ID: "a-1", Score: 0.95, Recordings: []identification.AcoustIDRecording{{ID: "rec", Sources: 3}}}}}
	cached := identification.NewCachedAcoustID(sqlDB, upstream)

	first, err := cached.Lookup(context.Background(), sampleFingerprint)
	if err != nil {
		t.Fatalf("first Lookup() error = %v", err)
	}
	second, err := cached.Lookup(context.Background(), sampleFingerprint)
	if err != nil {
		t.Fatalf("second Lookup() error = %v", err)
	}

	if upstream.calls != 1 {
		t.Fatalf("upstream calls = %d, want 1", upstream.calls)
	}
	if len(first) != 1 || len(second) != 1 || second[0].Recordings[0].ID != "rec" || second[0].Score != 0.95 {
		t.Fatalf("first = %+v second = %+v", first, second)
	}

	other := identification.Fingerprint{DurationSeconds: 212, Value: "DIFFERENT"}
	if _, err := cached.Lookup(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	if upstream.calls != 2 {
		t.Fatalf("upstream calls = %d, want 2 after a different fingerprint", upstream.calls)
	}
}

func TestCachedAcoustIDCachesEmptyResultsButNotFailures(t *testing.T) {
	sqlDB := testutil.OpenMigratedDB(t)
	upstream := &fakeAcoustID{err: identification.ErrServiceUnavailable}
	cached := identification.NewCachedAcoustID(sqlDB, upstream)

	if _, err := cached.Lookup(context.Background(), sampleFingerprint); !errors.Is(err, identification.ErrServiceUnavailable) {
		t.Fatalf("error = %v", err)
	}
	upstream.err = nil
	if _, err := cached.Lookup(context.Background(), sampleFingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err := cached.Lookup(context.Background(), sampleFingerprint); err != nil {
		t.Fatal(err)
	}

	if upstream.calls != 2 {
		t.Fatalf("upstream calls = %d, want 2 (failure not cached, empty result cached)", upstream.calls)
	}
}

func TestCachedMusicBrainzFetchesRecordingOnce(t *testing.T) {
	sqlDB := testutil.OpenMigratedDB(t)
	upstream := &fakeMusicBrainz{recordings: map[string]identification.Recording{
		"rec": {ID: "rec", Title: "Welcome to New York", ISRCs: []string{"USUG12306672"}, Releases: []identification.Release{{ID: "rel", Title: "1989", Position: identification.Position{DiscNumber: 1, TrackNumber: 1}}}},
	}}
	cached := identification.NewCachedMusicBrainz(sqlDB, upstream)

	if _, err := cached.Recording(context.Background(), "rec"); err != nil {
		t.Fatal(err)
	}
	recording, err := cached.Recording(context.Background(), "rec")
	if err != nil {
		t.Fatal(err)
	}

	if len(upstream.requested) != 1 {
		t.Fatalf("upstream requests = %v, want one", upstream.requested)
	}
	if recording.Title != "Welcome to New York" || recording.ISRCs[0] != "USUG12306672" || recording.Releases[0].Position.TrackNumber != 1 {
		t.Fatalf("recording = %+v", recording)
	}
}

func TestCachedMusicBrainzDoesNotCacheNotFoundOrFailures(t *testing.T) {
	sqlDB := testutil.OpenMigratedDB(t)
	upstream := &fakeMusicBrainz{recordings: map[string]identification.Recording{}}
	cached := identification.NewCachedMusicBrainz(sqlDB, upstream)

	if _, err := cached.Recording(context.Background(), "gone"); !errors.Is(err, identification.ErrRecordingNotFound) {
		t.Fatalf("error = %v", err)
	}
	upstream.recordings["gone"] = identification.Recording{ID: "gone", Title: "Now present"}
	recording, err := cached.Recording(context.Background(), "gone")
	if err != nil || recording.Title != "Now present" {
		t.Fatalf("recording = %+v err = %v", recording, err)
	}
	if len(upstream.requested) != 2 {
		t.Fatalf("upstream requests = %v", upstream.requested)
	}
}

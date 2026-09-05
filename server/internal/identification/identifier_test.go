package identification_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/identification"
)

type fakeFingerprinter struct {
	fingerprint identification.Fingerprint
	err         error
}

func (fake fakeFingerprinter) Fingerprint(_ context.Context, _ string) (identification.Fingerprint, error) {
	return fake.fingerprint, fake.err
}

type fakeAcoustID struct {
	results []identification.AcoustIDResult
	err     error
	calls   int
}

func (fake *fakeAcoustID) Lookup(_ context.Context, _ identification.Fingerprint) ([]identification.AcoustIDResult, error) {
	fake.calls++
	return fake.results, fake.err
}

type fakeMusicBrainz struct {
	recordings map[string]identification.Recording
	byISRC     map[string][]string
	err        error
	requested  []string
	isrcs      []string
}

func (fake *fakeMusicBrainz) RecordingIDsByISRC(_ context.Context, isrc string) ([]string, error) {
	fake.isrcs = append(fake.isrcs, isrc)
	if fake.err != nil {
		return nil, fake.err
	}
	ids, ok := fake.byISRC[isrc]
	if !ok {
		return nil, identification.ErrRecordingNotFound
	}
	return ids, nil
}

func (fake *fakeMusicBrainz) Recording(_ context.Context, mbid string) (identification.Recording, error) {
	fake.requested = append(fake.requested, mbid)
	if fake.err != nil {
		return identification.Recording{}, fake.err
	}
	recording, ok := fake.recordings[mbid]
	if !ok {
		return identification.Recording{}, identification.ErrRecordingNotFound
	}
	return recording, nil
}

var sampleFingerprint = identification.Fingerprint{DurationSeconds: 212, Value: "AQAD"}

func newIdentifier(fingerprinter fakeFingerprinter, acoustID *fakeAcoustID, musicBrainz *fakeMusicBrainz) *identification.Identifier {
	return identification.NewIdentifierWithSources(fingerprinter, acoustID, musicBrainz, 0.90)
}

func TestIdentifyPicksHighestScoreThenMostSources(t *testing.T) {
	acoustID := &fakeAcoustID{results: []identification.AcoustIDResult{
		{ID: "a-1", Score: 0.95, Recordings: []identification.AcoustIDRecording{{ID: "rec-few", Sources: 3}, {ID: "rec-many", Sources: 41}}},
		{ID: "a-2", Score: 0.99, Recordings: nil},
		{ID: "a-3", Score: 0.60, Recordings: []identification.AcoustIDRecording{{ID: "rec-other", Sources: 90}}},
	}}
	musicBrainz := &fakeMusicBrainz{recordings: map[string]identification.Recording{"rec-many": {ID: "rec-many", Title: "Welcome to New York"}}}

	result := newIdentifier(fakeFingerprinter{fingerprint: sampleFingerprint}, acoustID, musicBrainz).Identify(context.Background(), "song.flac", identification.Hint{})

	if result.Outcome != identification.OUTCOME_MATCHED {
		t.Fatalf("outcome = %s (%s), want matched", result.Outcome, result.Reason)
	}
	if result.Recording == nil || result.Recording.ID != "rec-many" || result.Score != 0.95 || result.AcoustID != "a-1" {
		t.Fatalf("result = %+v", result)
	}
	if len(musicBrainz.requested) != 1 {
		t.Fatalf("MusicBrainz requested %v, want exactly one lookup", musicBrainz.requested)
	}
}

func TestIdentifyReportsBelowThreshold(t *testing.T) {
	acoustID := &fakeAcoustID{results: []identification.AcoustIDResult{
		{ID: "a-1", Score: 0.71, Recordings: []identification.AcoustIDRecording{{ID: "rec", Sources: 5}}},
	}}
	musicBrainz := &fakeMusicBrainz{}

	result := newIdentifier(fakeFingerprinter{fingerprint: sampleFingerprint}, acoustID, musicBrainz).Identify(context.Background(), "song.flac", identification.Hint{})

	if result.Outcome != identification.OUTCOME_BELOW_THRESHOLD || result.Score != 0.71 || result.Recording != nil {
		t.Fatalf("result = %+v", result)
	}
	if len(musicBrainz.requested) != 0 {
		t.Fatalf("MusicBrainz must not be called below threshold, got %v", musicBrainz.requested)
	}
}

func TestIdentifyReportsNoMatchWhenAcoustIDKnowsNothing(t *testing.T) {
	result := newIdentifier(fakeFingerprinter{fingerprint: sampleFingerprint}, &fakeAcoustID{}, &fakeMusicBrainz{}).Identify(context.Background(), "song.flac", identification.Hint{})

	if result.Outcome != identification.OUTCOME_NO_MATCH {
		t.Fatalf("result = %+v", result)
	}
}

func TestIdentifyTreatsUnknownRecordingAsNoMatch(t *testing.T) {
	acoustID := &fakeAcoustID{results: []identification.AcoustIDResult{
		{ID: "a-1", Score: 0.97, Recordings: []identification.AcoustIDRecording{{ID: "gone", Sources: 5}}},
	}}

	result := newIdentifier(fakeFingerprinter{fingerprint: sampleFingerprint}, acoustID, &fakeMusicBrainz{}).Identify(context.Background(), "song.flac", identification.Hint{})

	if result.Outcome != identification.OUTCOME_NO_MATCH || result.Reason == "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestIdentifyReportsUnavailableServicesWithReason(t *testing.T) {
	cases := map[string]struct {
		fingerprinter fakeFingerprinter
		acoustID      *fakeAcoustID
		musicBrainz   *fakeMusicBrainz
		want          identification.Outcome
	}{
		"fpcalc missing": {
			fakeFingerprinter{err: identification.ErrFingerprinterUnavailable}, &fakeAcoustID{}, &fakeMusicBrainz{},
			identification.OUTCOME_UNAVAILABLE,
		},
		"fingerprint failed": {
			fakeFingerprinter{err: errors.Join(identification.ErrFingerprintFailed, errors.New("ERROR: Empty fingerprint"))}, &fakeAcoustID{}, &fakeMusicBrainz{},
			identification.OUTCOME_UNAVAILABLE,
		},
		"acoustid down": {
			fakeFingerprinter{fingerprint: sampleFingerprint}, &fakeAcoustID{err: identification.ErrServiceUnavailable}, &fakeMusicBrainz{},
			identification.OUTCOME_UNAVAILABLE,
		},
		"musicbrainz down": {
			fakeFingerprinter{fingerprint: sampleFingerprint},
			&fakeAcoustID{results: []identification.AcoustIDResult{{ID: "a", Score: 0.99, Recordings: []identification.AcoustIDRecording{{ID: "rec", Sources: 1}}}}},
			&fakeMusicBrainz{err: identification.ErrServiceUnavailable},
			identification.OUTCOME_UNAVAILABLE,
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			result := newIdentifier(testCase.fingerprinter, testCase.acoustID, testCase.musicBrainz).Identify(context.Background(), "song.flac", identification.Hint{})
			if result.Outcome != testCase.want || result.Reason == "" {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestIdentifyUsesTaggedRecordingIDBeforeFingerprinting(t *testing.T) {
	acoustID := &fakeAcoustID{}
	musicBrainz := &fakeMusicBrainz{recordings: map[string]identification.Recording{"rec-tag": {ID: "rec-tag", Title: "Tagged"}}}
	fingerprinter := fakeFingerprinter{err: identification.ErrFingerprinterUnavailable}

	result := newIdentifier(fingerprinter, acoustID, musicBrainz).Identify(context.Background(), "song.flac", identification.Hint{RecordingID: "rec-tag", ISRC: "USUG12306672"})

	if result.Outcome != identification.OUTCOME_MATCHED || result.Method != identification.METHOD_TAG_RECORDING_ID || result.Recording.ID != "rec-tag" {
		t.Fatalf("result = %+v", result)
	}
	if acoustID.calls != 0 || len(musicBrainz.isrcs) != 0 {
		t.Fatalf("tagged MBID must skip AcoustID and ISRC lookups: acoustid=%d isrc=%v", acoustID.calls, musicBrainz.isrcs)
	}
}

func TestIdentifyFallsBackToISRCThenFingerprint(t *testing.T) {
	acoustID := &fakeAcoustID{results: []identification.AcoustIDResult{{ID: "a", Score: 0.99, Recordings: []identification.AcoustIDRecording{{ID: "rec-fp", Sources: 1}}}}}
	musicBrainz := &fakeMusicBrainz{
		recordings: map[string]identification.Recording{"rec-isrc": {ID: "rec-isrc"}, "rec-fp": {ID: "rec-fp"}},
		byISRC:     map[string][]string{"USUG12306672": {"rec-isrc"}},
	}

	viaISRC := newIdentifier(fakeFingerprinter{fingerprint: sampleFingerprint}, acoustID, musicBrainz).Identify(context.Background(), "song.flac", identification.Hint{RecordingID: "gone", ISRC: "USUG12306672"})
	if viaISRC.Outcome != identification.OUTCOME_MATCHED || viaISRC.Method != identification.METHOD_TAG_ISRC || viaISRC.Recording.ID != "rec-isrc" {
		t.Fatalf("via ISRC = %+v", viaISRC)
	}
	if acoustID.calls != 0 {
		t.Fatalf("ISRC match must not fingerprint, acoustid calls = %d", acoustID.calls)
	}

	viaFingerprint := newIdentifier(fakeFingerprinter{fingerprint: sampleFingerprint}, acoustID, musicBrainz).Identify(context.Background(), "song.flac", identification.Hint{ISRC: "UNKNOWN00001"})
	if viaFingerprint.Outcome != identification.OUTCOME_MATCHED || viaFingerprint.Method != identification.METHOD_FINGERPRINT || viaFingerprint.Recording.ID != "rec-fp" {
		t.Fatalf("via fingerprint = %+v", viaFingerprint)
	}
}

func TestIdentifyReportsUnavailableWhenTagLookupFails(t *testing.T) {
	musicBrainz := &fakeMusicBrainz{err: identification.ErrServiceUnavailable}

	result := newIdentifier(fakeFingerprinter{fingerprint: sampleFingerprint}, &fakeAcoustID{}, musicBrainz).Identify(context.Background(), "song.flac", identification.Hint{RecordingID: "rec-tag"})

	if result.Outcome != identification.OUTCOME_UNAVAILABLE || result.Reason == "" {
		t.Fatalf("result = %+v", result)
	}
}

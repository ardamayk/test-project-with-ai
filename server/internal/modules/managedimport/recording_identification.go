package managedimport

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/ardam/navidrome-replacement/server/internal/identification"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
)

// MetadataSource says which source produced a Track's metadata (ADR 0017).
type MetadataSource string

const (
	METADATA_SOURCE_FILE_TAGS   MetadataSource = "file_tags"
	METADATA_SOURCE_MUSICBRAINZ MetadataSource = "musicbrainz"
)

// recordingIdentifier is the seam Managed Import uses to identify a staged
// file; the identification package provides the real one.
type recordingIdentifier interface {
	Identify(ctx context.Context, path string, hint identification.Hint) identification.Identification
}

// IdentificationPreview tells the user, per file, which system produced the
// metadata and why, so Recording Identification can be inspected and tested
// from the Import Preview and Import History.
type IdentificationPreview struct {
	Source        MetadataSource        `json:"source"`
	Outcome       IdentificationOutcome `json:"outcome"`
	Method        identification.Method `json:"method,omitempty"`
	Reason        string                `json:"reason,omitempty"`
	AcoustIDScore float64               `json:"acoustIdScore,omitempty"`
	RecordingID   string                `json:"recordingId,omitempty"`
	ISRC          string                `json:"isrc,omitempty"`
	ChangedFields []string              `json:"changedFields,omitempty"`
}

// identificationRecord is what a job stores between Import Preview and
// commit: the preview summary plus the MusicBrainz Recording needed to
// repeat the merge over the fresh inspection at commit time.
type identificationRecord struct {
	IdentificationPreview
	Recording *identification.Recording `json:"recording,omitempty"`
}

// IdentificationOutcome extends identification.Outcome with the two reasons
// Managed Import itself never asked: the batch switch was off, or the
// feature is inactive on this Music Server.
type IdentificationOutcome string

const (
	IDENTIFICATION_OUTCOME_SWITCHED_OFF IdentificationOutcome = "switched_off"
	IDENTIFICATION_OUTCOME_INACTIVE     IdentificationOutcome = "inactive"
)

func decodeIdentificationRecord(encoded string) (identificationRecord, error) {
	record := identificationRecord{IdentificationPreview: IdentificationPreview{Source: METADATA_SOURCE_FILE_TAGS}}
	if encoded == "" {
		return record, nil
	}
	if err := json.Unmarshal([]byte(encoded), &record); err != nil {
		return identificationRecord{}, fmt.Errorf("decode stored Recording Identification: %w", err)
	}
	if record.Source == "" {
		record.Source = METADATA_SOURCE_FILE_TAGS
	}
	return record, nil
}

// identifyStagedUpload runs Recording Identification for one staged file
// when the Import Batch asked for it and the feature is active, and merges
// a confident match over the file's tags. It never fails: every reason the
// tags stayed untouched is returned in the record.
func (service *Service) identifyStagedUpload(ctx context.Context, job importJob, stagedPath string, inspection library.MediaInspection) (identificationRecord, library.MediaInspection, error) {
	// Identifiers already in the tags are kept even when no lookup runs, so
	// Recording Duplicates are found offline and the ISRC is stored.
	record := identificationRecord{IdentificationPreview: IdentificationPreview{
		Source:      METADATA_SOURCE_FILE_TAGS,
		RecordingID: inspection.Metadata.MusicBrainzRecordingID,
		ISRC:        inspection.Metadata.ISRC,
	}}
	if job.BatchID != "" {
		batch, err := service.store.GetBatch(ctx, job.BatchID)
		if err != nil {
			return record, inspection, err
		}
		if !batch.RecordingIdentification {
			record.Outcome = IDENTIFICATION_OUTCOME_SWITCHED_OFF
			record.Reason = "Recording Identification was switched off for this Import Batch"
			return record, inspection, nil
		}
	}
	if service.identifier == nil {
		record.Outcome = IDENTIFICATION_OUTCOME_INACTIVE
		record.Reason = "Recording Identification is not active on this Music Server"
		return record, inspection, nil
	}
	result := service.identifier.Identify(ctx, stagedPath, identification.Hint{
		RecordingID: inspection.Metadata.MusicBrainzRecordingID,
		ISRC:        inspection.Metadata.ISRC,
	})
	record.Outcome = IdentificationOutcome(result.Outcome)
	record.Method = result.Method
	record.Reason = result.Reason
	record.AcoustIDScore = result.Score
	if result.Outcome != identification.OUTCOME_MATCHED || result.Recording == nil {
		return record, inspection, nil
	}
	record.Source = METADATA_SOURCE_MUSICBRAINZ
	record.RecordingID = result.Recording.ID
	record.Recording = result.Recording
	if len(result.Recording.ISRCs) > 0 {
		record.ISRC = result.Recording.ISRCs[0]
	}
	merged, changed := mergeRecording(inspection.Metadata, *result.Recording)
	record.ChangedFields = changed
	inspection.Metadata = merged
	return record, inspection, nil
}

// applyStoredIdentification repeats the merge at commit time over the fresh
// inspection of the staged file, so the committed Track carries exactly what
// the Import Preview showed.
func applyStoredIdentification(job importJob, inspection library.MediaInspection) (identificationRecord, library.MediaInspection, error) {
	record, err := decodeIdentificationRecord(job.IdentificationJSON)
	if err != nil {
		return record, inspection, fmt.Errorf("job %q: %w", job.ID, err)
	}
	if record.Source != METADATA_SOURCE_MUSICBRAINZ || record.Recording == nil {
		return record, inspection, nil
	}
	merged, changed := mergeRecording(inspection.Metadata, *record.Recording)
	record.ChangedFields = changed
	inspection.Metadata = merged
	return record, inspection, nil
}

func (record identificationRecord) encode() (string, error) {
	encoded, err := json.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("encode Recording Identification: %w", err)
	}
	return string(encoded), nil
}

// mergeRecording applies ADR 0017's rule: recording-level fields (Title,
// Artists) come from MusicBrainz; release-level fields stay as tagged and are
// only completed from the closest release when the tag left them empty.
//
// The Strict Import Profile already requires Title, Artists, Album, Album
// Artists, Genre and positions, so today only Year can be empty; track and
// disc totals are left alone so the album position check that ran on the
// tags at Import Preview still holds at commit.
func mergeRecording(tagged library.NormalizedMediaMetadata, recording identification.Recording) (library.NormalizedMediaMetadata, []string) {
	merged := tagged
	var changed []string
	if recording.Title != "" && recording.Title != tagged.Title {
		merged.Title = recording.Title
		changed = append(changed, "title")
	}
	if artists := creditNames(recording.Artists); len(artists) > 0 && !slices.Equal(artists, tagged.Artists) {
		merged.Artists = artists
		changed = append(changed, "artists")
	}
	if release, hasRelease := chooseRelease(recording.Releases, tagged.Album); hasRelease && tagged.Year == 0 && release.Year > 0 {
		merged.Year = release.Year
		changed = append(changed, "year")
	}
	return merged, changed
}

// normalizeReleaseTitle folds typographic apostrophes and quotes so a tag
// written with ' matches a MusicBrainz title written with ’.
func normalizeReleaseTitle(title string) string {
	return normalizeIdentity(apostropheFolder.Replace(title))
}

var apostropheFolder = strings.NewReplacer("\u2019", "'", "\u2018", "'", "\u201c", "\"", "\u201d", "\"")

func creditNames(credits []identification.Credit) []string {
	names := make([]string, 0, len(credits))
	for _, credit := range credits {
		if credit.Name != "" {
			names = append(names, credit.Name)
		}
	}
	return names
}

// chooseRelease prefers the release whose title equals the tagged Album,
// then the earliest official album release, then the first release.
func chooseRelease(releases []identification.Release, taggedAlbum string) (identification.Release, bool) {
	if len(releases) == 0 {
		return identification.Release{}, false
	}
	if wanted := normalizeReleaseTitle(taggedAlbum); wanted != "" {
		for _, release := range releases {
			if normalizeReleaseTitle(release.Title) == wanted {
				return release, true
			}
		}
	}
	chosen, found := identification.Release{}, false
	for _, release := range releases {
		if release.Status != "Official" || release.ReleaseGroup.PrimaryType != "Album" {
			continue
		}
		if !found || (release.Date != "" && (chosen.Date == "" || release.Date < chosen.Date)) {
			chosen, found = release, true
		}
	}
	if found {
		return chosen, true
	}
	return releases[0], true
}

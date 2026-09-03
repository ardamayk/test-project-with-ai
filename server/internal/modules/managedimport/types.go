package managedimport

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
)

const (
	STATUS_UPLOADING                  ImportStatus = "uploading"
	STATUS_AWAITING_CONFIRMATION      ImportStatus = "awaiting_confirmation"
	STATUS_COMMITTED                  ImportStatus = "committed"
	STATUS_FAILED                     ImportStatus = "failed"
	MAX_CONFIRMATION_BODY_BYTES       int64        = 4 * 1024
	MAX_BATCH_CONFIRMATION_BODY_BYTES              = 256 * 1024
	MAX_JOB_CREATE_BODY_BYTES         int64        = 4 * 1024
	MAX_ORIGINAL_FILENAME_BYTES                    = 255
	BITS_PER_KILOBIT                               = 1000
	VALIDATION_CLEANUP_TIMEOUT                     = 5 * time.Second
	ERROR_CODE_EXACT_DUPLICATE                     = "exact_duplicate"
	ERROR_CODE_REVISION_CONFLICT                   = "import_revision_conflict"
	UPLOAD_INTERRUPTED_ERROR_CODE                  = "upload_interrupted"
	IMPORT_CANCELED_RESULT_CODE                    = "canceled"
	IMPORT_INACTIVITY_TIMEOUT                      = 15 * time.Minute
	IMPORT_CLEANUP_INTERVAL                        = time.Minute
	IMPORT_HISTORY_LIMIT                           = 20
)

type BatchStatus string

const (
	BATCH_STATUS_UPLOADING  BatchStatus = "uploading"
	BATCH_STATUS_CONFIRMING BatchStatus = "confirming"
	BATCH_STATUS_COMPLETED  BatchStatus = "completed"
)

type BatchFileState string

const (
	BATCH_FILE_ACCEPTED   BatchFileState = "accepted"
	BATCH_FILE_REJECTED   BatchFileState = "rejected"
	BATCH_FILE_UNRESOLVED BatchFileState = "unresolved"
	BATCH_FILE_COMPLETED  BatchFileState = "completed"
)

type ImportOutcome string

const (
	OUTCOME_IMPORTED      ImportOutcome = "imported"
	OUTCOME_REJECTED      ImportOutcome = "rejected"
	OUTCOME_FAILED        ImportOutcome = "failed"
	OUTCOME_REPLACED      ImportOutcome = "replaced"
	OUTCOME_NOT_ATTEMPTED ImportOutcome = "not_attempted"
)

type ImportStatus string

type DuplicateClassification string

const (
	DUPLICATE_NONE     DuplicateClassification = "none"
	DUPLICATE_EXACT    DuplicateClassification = "exact_duplicate"
	DUPLICATE_POSSIBLE DuplicateClassification = "possible_duplicate"
)

type commitPhase string

const (
	COMMIT_PHASE_PREPARED           commitPhase = "prepared"
	COMMIT_PHASE_PLACED             commitPhase = "placed"
	COMMIT_PHASE_VERIFIED           commitPhase = "verified"
	COMMIT_PHASE_DATABASE_COMMITTED commitPhase = "database_committed"
	COMMIT_PHASE_CLEANED            commitPhase = "cleaned"
	COMMIT_PHASE_COMPLETED          commitPhase = "completed"
	COMMIT_PHASE_ROLLED_BACK        commitPhase = "rolled_back"
)

// migrationPhase marks the durable progress of a Library Migration copy. The
// database_committed phase only exists in memory: the copy row is deleted by
// the cutover transaction that reaches it.
type migrationPhase string

const (
	MIGRATION_PHASE_PREPARED           migrationPhase = "prepared"
	MIGRATION_PHASE_VERIFIED           migrationPhase = "verified"
	MIGRATION_PHASE_PROMOTED           migrationPhase = "promoted"
	MIGRATION_PHASE_DATABASE_COMMITTED migrationPhase = "database_committed"
)

type HistoryResultCode string

const (
	HISTORY_RESULT_COMPLETED           HistoryResultCode = "completed"
	HISTORY_RESULT_PARTIALLY_COMPLETED HistoryResultCode = "partially_completed"
	HISTORY_RESULT_FAILED              HistoryResultCode = "failed"
	HISTORY_RESULT_CANCELED            HistoryResultCode = "canceled"
)

var (
	ErrNotFound            = errors.New("managed import job not found")
	ErrInvalidState        = errors.New("managed import job is not awaiting this operation")
	ErrRevisionConflict    = errors.New("managed import revision conflict")
	ErrExactDuplicate      = errors.New("managed import file exactly duplicates a committed Track")
	ErrUploadTooLarge      = errors.New("managed import file exceeds upload limit")
	ErrBatchTooLarge       = errors.New("managed import batch exceeds upload limit")
	ErrUploadInterrupted   = errors.New("managed import upload was interrupted")
	ErrInvalidUpload       = errors.New("managed import upload is invalid")
	ErrInsufficientStorage = errors.New("managed storage capacity is insufficient")
	ErrUnsafeStoragePath   = errors.New("managed storage path is unsafe")
	ErrMigrationInProgress = errors.New("library migration preview is already in progress")
	ErrCleanupConflict     = errors.New("legacy source cleanup preview changed")
	ErrTrackNotFound       = errors.New("track not found")
	ErrNotManagedTrack     = errors.New("track is not managed")
	ErrDeletionConflict    = errors.New("permanent track deletion preview changed")
	ErrReplacementConflict = errors.New("track replacement preview changed")
	ErrNotReplacementJob   = errors.New("managed import job does not replace a track")
	ErrReplacementRequired = errors.New("managed import job requires track replacement confirmation")
)

type Job struct {
	ID                 string       `json:"id"`
	Status             ImportStatus `json:"status"`
	Revision           int          `json:"revision"`
	ValidationProgress int          `json:"validationProgress"`
	ErrorCode          string       `json:"errorCode,omitempty"`
	TrackID            string       `json:"trackId,omitempty"`
	ReplacesTrackID    string       `json:"replacesTrackId,omitempty"`
}

type JobCreate struct {
	BatchID      string `json:"batchId"`
	ClientFileID string `json:"clientFileId"`
}

type Batch struct {
	ID       string      `json:"id"`
	Status   BatchStatus `json:"status"`
	Revision int         `json:"revision"`
	Files    []BatchFile `json:"files"`
}

type BatchFile struct {
	JobID              string         `json:"jobId"`
	ClientFileID       string         `json:"clientFileId,omitempty"`
	State              BatchFileState `json:"state"`
	Status             ImportStatus   `json:"status"`
	Revision           int            `json:"revision"`
	ValidationProgress int            `json:"validationProgress"`
	OriginalFilename   string         `json:"originalFilename,omitempty"`
	Selected           bool           `json:"selected"`
	Preview            *Preview       `json:"preview,omitempty"`
	ErrorCode          string         `json:"errorCode,omitempty"`
	ErrorField         string         `json:"errorField,omitempty"`
	ErrorReason        string         `json:"errorReason,omitempty"`
	Outcome            ImportOutcome  `json:"outcome,omitempty"`
	TrackID            string         `json:"trackId,omitempty"`
}

type BatchConfirmation struct {
	Revision           int                 `json:"revision"`
	SelectedFileIDs    []string            `json:"selectedFileIds"`
	DuplicateDecisions []DuplicateDecision `json:"duplicateDecisions,omitempty"`
}

type DuplicateDecision struct {
	JobID  string          `json:"jobId"`
	Action DuplicateAction `json:"action"`
}

type DuplicateAction string

const (
	DUPLICATE_ACTION_IMPORT_SEPARATELY DuplicateAction = "import_separately"
	DUPLICATE_ACTION_REPLACE_EXISTING  DuplicateAction = "replace_existing"
	DUPLICATE_ACTION_DO_NOT_IMPORT     DuplicateAction = "do_not_import"
)

type Preview struct {
	JobID                   string                   `json:"jobId"`
	Status                  ImportStatus             `json:"status"`
	Revision                int                      `json:"revision"`
	File                    PreviewFile              `json:"file"`
	DuplicateClassification DuplicateClassification  `json:"duplicateClassification"`
	DuplicateCandidates     []DuplicateCandidate     `json:"duplicateCandidates,omitempty"`
	Replacement             *TrackReplacementPreview `json:"replacement,omitempty"`
}

type DuplicateCandidate struct {
	TrackID    string   `json:"trackId"`
	Title      string   `json:"title"`
	Artists    []string `json:"artists"`
	Album      string   `json:"album"`
	DiscNo     int      `json:"discNo"`
	TrackNo    int      `json:"trackNo"`
	Format     string   `json:"format"`
	DurationMs int      `json:"durationMs"`
}

type PreviewFile struct {
	OriginalFilename string   `json:"originalFilename"`
	Title            string   `json:"title"`
	Artists          []string `json:"artists"`
	AlbumArtists     []string `json:"albumArtists"`
	Album            string   `json:"album"`
	Genres           []string `json:"genres"`
	TrackNo          int      `json:"trackNo"`
	TrackTotal       int      `json:"trackTotal,omitempty"`
	DiscNo           int      `json:"discNo"`
	DiscTotal        int      `json:"discTotal,omitempty"`
	Year             int      `json:"year,omitempty"`
	DurationMs       int      `json:"durationMs"`
	Format           string   `json:"format"`
	Container        string   `json:"container"`
	Codec            string   `json:"codec"`
	SampleRateHz     int      `json:"sampleRateHz"`
	ChannelCount     int      `json:"channelCount"`
	BitDepth         int      `json:"bitDepth,omitempty"`
	BitrateKbps      int      `json:"bitrateKbps"`
	ArtworkMediaType string   `json:"artworkMediaType"`
}

type MigrationFileState string

const (
	MIGRATION_FILE_ACCEPTED MigrationFileState = "accepted"
	MIGRATION_FILE_REJECTED MigrationFileState = "rejected"
)

type MigrationPreview struct {
	AcceptedCount int                    `json:"acceptedCount"`
	RejectedCount int                    `json:"rejectedCount"`
	Files         []MigrationPreviewFile `json:"files"`
}

type MigrationPreviewFile struct {
	TrackID          string             `json:"trackId"`
	OriginalFilename string             `json:"originalFilename"`
	State            MigrationFileState `json:"state"`
	Preview          *PreviewFile       `json:"preview,omitempty"`
	ErrorCode        string             `json:"errorCode,omitempty"`
	ErrorField       string             `json:"errorField,omitempty"`
	ErrorReason      string             `json:"errorReason,omitempty"`
}

type MigrationStageState string

const (
	MIGRATION_STAGE_VERIFIED MigrationStageState = "verified"
	MIGRATION_STAGE_REJECTED MigrationStageState = "rejected"
	MIGRATION_STAGE_FAILED   MigrationStageState = "failed"
)

type MigrationCutoverState string

const (
	MIGRATION_CUTOVER_MIGRATED      MigrationCutoverState = "migrated"
	MIGRATION_CUTOVER_REJECTED      MigrationCutoverState = "rejected"
	MIGRATION_CUTOVER_FAILED        MigrationCutoverState = "failed"
	MIGRATION_CUTOVER_NOT_ATTEMPTED MigrationCutoverState = "not_attempted"
)

const ERROR_CODE_MIGRATION_SOURCE_INACTIVE = "legacy_source_inactive"

type MigrationCutover struct {
	MigratedCount     int                    `json:"migratedCount"`
	RejectedCount     int                    `json:"rejectedCount"`
	FailedCount       int                    `json:"failedCount"`
	NotAttemptedCount int                    `json:"notAttemptedCount"`
	Files             []MigrationCutoverFile `json:"files"`
}

type MigrationCutoverFile struct {
	TrackID          string                `json:"trackId"`
	OriginalFilename string                `json:"originalFilename"`
	State            MigrationCutoverState `json:"state"`
	CreatedTrackID   string                `json:"createdTrackId,omitempty"`
	ContentSHA256    string                `json:"contentSha256,omitempty"`
	ErrorCode        string                `json:"errorCode,omitempty"`
	ErrorField       string                `json:"errorField,omitempty"`
	ErrorReason      string                `json:"errorReason,omitempty"`
}

type MigrationCleanupState string

const (
	MIGRATION_CLEANUP_ELIGIBLE   MigrationCleanupState = "eligible"
	MIGRATION_CLEANUP_INELIGIBLE MigrationCleanupState = "ineligible"
	MIGRATION_CLEANUP_DELETED    MigrationCleanupState = "deleted"
	MIGRATION_CLEANUP_FAILED     MigrationCleanupState = "failed"
)

// MigrationCleanupPreview lists every legacy source file with the exact
// count and total size of the files that may be deleted. Only sources proven
// to correspond to successfully migrated Managed Tracks are eligible.
type MigrationCleanupPreview struct {
	EligibleCount   int                           `json:"eligibleCount"`
	IneligibleCount int                           `json:"ineligibleCount"`
	TotalSizeBytes  int64                         `json:"totalSizeBytes"`
	Files           []MigrationCleanupPreviewFile `json:"files"`
}

type MigrationCleanupPreviewFile struct {
	TrackID          string                `json:"trackId"`
	SourceTrackID    string                `json:"sourceTrackId,omitempty"`
	OriginalFilename string                `json:"originalFilename"`
	State            MigrationCleanupState `json:"state"`
	SizeBytes        int64                 `json:"sizeBytes,omitempty"`
	ContentSHA256    string                `json:"contentSha256,omitempty"`
	ErrorCode        string                `json:"errorCode,omitempty"`
	ErrorField       string                `json:"errorField,omitempty"`
	ErrorReason      string                `json:"errorReason,omitempty"`
}

// MigrationCleanupConfirmation names the exact Managed Tracks whose legacy
// sources are to be deleted together with the file count and total size the
// user confirmed; any mismatch rejects the whole request.
type MigrationCleanupConfirmation struct {
	TrackIDs       []string `json:"trackIds"`
	FileCount      int      `json:"fileCount"`
	TotalSizeBytes int64    `json:"totalSizeBytes"`
}

type MigrationCleanup struct {
	DeletedCount         int                    `json:"deletedCount"`
	FailedCount          int                    `json:"failedCount"`
	DeletedBytes         int64                  `json:"deletedBytes"`
	PrunedDirectoryCount int                    `json:"prunedDirectoryCount"`
	Files                []MigrationCleanupFile `json:"files"`
}

type MigrationCleanupFile struct {
	TrackID          string                `json:"trackId"`
	SourceTrackID    string                `json:"sourceTrackId"`
	OriginalFilename string                `json:"originalFilename"`
	State            MigrationCleanupState `json:"state"`
	SizeBytes        int64                 `json:"sizeBytes,omitempty"`
	ErrorCode        string                `json:"errorCode,omitempty"`
	ErrorField       string                `json:"errorField,omitempty"`
	ErrorReason      string                `json:"errorReason,omitempty"`
}

type MigrationStage struct {
	VerifiedCount int                  `json:"verifiedCount"`
	RejectedCount int                  `json:"rejectedCount"`
	FailedCount   int                  `json:"failedCount"`
	Files         []MigrationStageFile `json:"files"`
}

type MigrationStageFile struct {
	TrackID          string              `json:"trackId"`
	OriginalFilename string              `json:"originalFilename"`
	State            MigrationStageState `json:"state"`
	PendingTrackID   string              `json:"pendingTrackId,omitempty"`
	PendingPath      string              `json:"-"`
	SourceSHA256     string              `json:"sourceSha256,omitempty"`
	PendingSHA256    string              `json:"pendingSha256,omitempty"`
	ErrorCode        string              `json:"errorCode,omitempty"`
	ErrorField       string              `json:"errorField,omitempty"`
	ErrorReason      string              `json:"errorReason,omitempty"`
}

type Confirmation struct {
	Revision          int             `json:"revision"`
	DuplicateDecision DuplicateAction `json:"duplicateDecision,omitempty"`
}

type Result struct {
	JobID    string       `json:"jobId"`
	Status   ImportStatus `json:"status"`
	Revision int          `json:"revision"`
	TrackID  string       `json:"trackId"`
}

type HistoryList struct {
	Items []HistoryItem `json:"items"`
}

type HistoryItem struct {
	ImportID    string            `json:"importId"`
	StartedAt   time.Time         `json:"startedAt"`
	CompletedAt time.Time         `json:"completedAt"`
	ResultCode  HistoryResultCode `json:"resultCode"`
	Counts      HistoryCounts     `json:"counts"`
	Files       []HistoryFile     `json:"files"`
}

type HistoryCounts struct {
	Total        int `json:"total"`
	Imported     int `json:"imported"`
	Rejected     int `json:"rejected"`
	Failed       int `json:"failed"`
	Replaced     int `json:"replaced"`
	NotAttempted int `json:"notAttempted"`
	Canceled     int `json:"canceled"`
}

type HistoryFile struct {
	FileID          string    `json:"fileId"`
	JobID           string    `json:"jobId"`
	SafeFilename    string    `json:"safeFilename,omitempty"`
	StartedAt       time.Time `json:"startedAt"`
	CompletedAt     time.Time `json:"completedAt"`
	ContentSHA256   string    `json:"contentSha256,omitempty"`
	ResultCode      string    `json:"resultCode"`
	CreatedTrackID  string    `json:"createdTrackId,omitempty"`
	ReplacedTrackID string    `json:"replacedTrackId,omitempty"`
}

type importJob struct {
	Job
	BatchID          string
	ClientFileID     string
	OriginalFilename string
	StagedFilePath   string
	ContentSHA256    string
	PreviewJSON      string
	ErrorField       string
	ErrorReason      string
	Outcome          ImportOutcome
	Selected         bool
	ReplaceTrackID   string
}

type commitJournal struct {
	ID               string
	JobID            string
	TrackID          string
	Phase            commitPhase
	StagedFilePath   string
	AudioFilePath    string
	ArtworkFilePath  string
	AudioSHA256      string
	ArtworkSHA256    string
	IsArtworkCreated bool
	RecoveryReason   string
}

type ValidationError struct {
	Code   string
	Field  string
	Reason string
	Err    error
}

func (validationErr *ValidationError) Error() string {
	if validationErr.Field == "" {
		return fmt.Sprintf("%s: %v", validationErr.Code, validationErr.Err)
	}
	return fmt.Sprintf("%s (%s): %v", validationErr.Code, validationErr.Field, validationErr.Err)
}

func (validationErr *ValidationError) Unwrap() error {
	return validationErr.Err
}

func validationError(err error) error {
	var inspectionErr *library.InspectionError
	if errors.As(err, &inspectionErr) {
		return &ValidationError{Code: string(inspectionErr.Code), Field: inspectionErr.Field, Reason: inspectionErr.Reason, Err: inspectionErr.Err}
	}
	return err
}

func previewFromInspection(job importJob, inspection library.MediaInspection) Preview {
	return Preview{
		JobID:                   job.ID,
		Status:                  job.Status,
		Revision:                job.Revision,
		File:                    previewFileFromInspection(job.OriginalFilename, inspection),
		DuplicateClassification: DUPLICATE_NONE,
	}
}

func previewFileFromInspection(originalFilename string, inspection library.MediaInspection) PreviewFile {
	metadata := inspection.Metadata
	audio := inspection.Audio
	return PreviewFile{
		OriginalFilename: originalFilename,
		Title:            metadata.Title,
		Artists:          metadata.Artists,
		AlbumArtists:     metadata.AlbumArtists,
		Album:            metadata.Album,
		Genres:           metadata.Genres,
		TrackNo:          metadata.TrackPosition.Number,
		TrackTotal:       metadata.TrackPosition.Total,
		DiscNo:           metadata.DiscPosition.Number,
		DiscTotal:        metadata.DiscPosition.Total,
		Year:             metadata.Year,
		DurationMs:       audio.DurationMs,
		Format:           audio.Format,
		Container:        audio.Container,
		Codec:            audio.Codec,
		SampleRateHz:     audio.SampleRateHz,
		ChannelCount:     audio.ChannelCount,
		BitDepth:         audio.BitDepth,
		BitrateKbps:      audio.BitrateKbps,
		ArtworkMediaType: inspection.AlbumArtwork.MIMEType,
	}
}

func batchFileFromJob(job importJob) (BatchFile, error) {
	file := BatchFile{
		JobID:              job.ID,
		ClientFileID:       job.ClientFileID,
		State:              BATCH_FILE_UNRESOLVED,
		Status:             job.Status,
		Revision:           job.Revision,
		ValidationProgress: job.ValidationProgress,
		OriginalFilename:   job.OriginalFilename,
		Selected:           job.Selected,
		ErrorCode:          job.ErrorCode,
		ErrorField:         job.ErrorField,
		ErrorReason:        job.ErrorReason,
		Outcome:            job.Outcome,
		TrackID:            job.TrackID,
	}
	if job.PreviewJSON != "" {
		var preview Preview
		if err := json.Unmarshal([]byte(job.PreviewJSON), &preview); err != nil {
			return BatchFile{}, fmt.Errorf("decode Import Preview for job %q: %w", job.ID, err)
		}
		file.Preview = &preview
	}
	switch {
	case job.Outcome == OUTCOME_REJECTED:
		file.State = BATCH_FILE_REJECTED
	case job.Outcome != "":
		file.State = BATCH_FILE_COMPLETED
	case job.Status == STATUS_AWAITING_CONFIRMATION:
		file.State = BATCH_FILE_ACCEPTED
	case job.Status == STATUS_FAILED:
		file.State = BATCH_FILE_REJECTED
	}
	return file, nil
}

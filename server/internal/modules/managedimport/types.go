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

var (
	ErrNotFound            = errors.New("managed import job not found")
	ErrInvalidState        = errors.New("managed import job is not awaiting this operation")
	ErrRevisionConflict    = errors.New("managed import revision conflict")
	ErrUploadTooLarge      = errors.New("managed import file exceeds upload limit")
	ErrBatchTooLarge       = errors.New("managed import batch exceeds upload limit")
	ErrInvalidUpload       = errors.New("managed import upload is invalid")
	ErrInsufficientStorage = errors.New("managed storage capacity is insufficient")
	ErrUnsafeStoragePath   = errors.New("managed storage path is unsafe")
)

type Job struct {
	ID                 string       `json:"id"`
	Status             ImportStatus `json:"status"`
	Revision           int          `json:"revision"`
	ValidationProgress int          `json:"validationProgress"`
	ErrorCode          string       `json:"errorCode,omitempty"`
	TrackID            string       `json:"trackId,omitempty"`
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
	Revision        int      `json:"revision"`
	SelectedFileIDs []string `json:"selectedFileIds"`
}

type Preview struct {
	JobID    string       `json:"jobId"`
	Status   ImportStatus `json:"status"`
	Revision int          `json:"revision"`
	File     PreviewFile  `json:"file"`
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

type Confirmation struct {
	Revision int `json:"revision"`
}

type Result struct {
	JobID    string       `json:"jobId"`
	Status   ImportStatus `json:"status"`
	Revision int          `json:"revision"`
	TrackID  string       `json:"trackId"`
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
	metadata := inspection.Metadata
	audio := inspection.Audio
	return Preview{
		JobID:    job.ID,
		Status:   job.Status,
		Revision: job.Revision,
		File: PreviewFile{
			OriginalFilename: job.OriginalFilename,
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
		},
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

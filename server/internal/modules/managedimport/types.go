package managedimport

import (
	"errors"
	"fmt"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
)

const (
	STATUS_UPLOADING             ImportStatus = "uploading"
	STATUS_AWAITING_CONFIRMATION ImportStatus = "awaiting_confirmation"
	STATUS_COMMITTED             ImportStatus = "committed"
	STATUS_FAILED                ImportStatus = "failed"
	MAX_UPLOAD_SIZE_BYTES        int64        = 2 * 1024 * 1024 * 1024
	MAX_CONFIRMATION_BODY_BYTES  int64        = 4 * 1024
	MAX_ORIGINAL_FILENAME_BYTES               = 255
	BITS_PER_KILOBIT                          = 1000
)

type ImportStatus string

var (
	ErrNotFound         = errors.New("managed import job not found")
	ErrInvalidState     = errors.New("managed import job is not awaiting this operation")
	ErrRevisionConflict = errors.New("managed import revision conflict")
	ErrUploadTooLarge   = errors.New("managed import file exceeds upload limit")
	ErrInvalidUpload    = errors.New("managed import upload is invalid")
)

type Job struct {
	ID       string       `json:"id"`
	Status   ImportStatus `json:"status"`
	Revision int          `json:"revision"`
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
	SampleRateHz     int      `json:"sampleRateHz"`
	ChannelCount     int      `json:"channelCount"`
	BitDepth         int      `json:"bitDepth"`
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
	OriginalFilename string
	StagedFilePath   string
	ContentSHA256    string
	TrackID          string
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
			SampleRateHz:     audio.SampleRateHz,
			ChannelCount:     audio.ChannelCount,
			BitDepth:         audio.BitDepth,
			BitrateKbps:      audio.BitrateKbps,
			ArtworkMediaType: inspection.AlbumArtwork.MIMEType,
		},
	}
}

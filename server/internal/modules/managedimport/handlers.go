package managedimport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/ardam/navidrome-replacement/server/internal/api/respond"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/go-chi/chi/v5"
)

const MIGRATION_PREVIEW_REQUEST_HEADER = "X-Migration-Preview"

type Handlers struct {
	service *Service
}

func NewHandlers(service *Service) *Handlers {
	return &Handlers{service: service}
}

func (handlers *Handlers) CreateJob(writer http.ResponseWriter, request *http.Request) {
	var creation JobCreate
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, MAX_JOB_CREATE_BODY_BYTES))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&creation); err != nil && !errors.Is(err, io.EOF) {
		respond.Error(writer, http.StatusBadRequest, "invalid_import_job", "Managed Import Job request is invalid")
		return
	}
	job, err := handlers.service.CreateJob(request.Context(), creation.BatchID, creation.ClientFileID)
	if err != nil {
		handleError(writer, request, err)
		return
	}
	respond.JSON(writer, http.StatusCreated, job)
}

func (handlers *Handlers) CreateBatch(writer http.ResponseWriter, request *http.Request) {
	batch, err := handlers.service.CreateBatch(request.Context())
	if err != nil {
		handleError(writer, request, err)
		return
	}
	respond.JSON(writer, http.StatusCreated, batch)
}

func (handlers *Handlers) GetBatch(writer http.ResponseWriter, request *http.Request) {
	batch, err := handlers.service.GetBatch(request.Context(), chi.URLParam(request, "batchId"))
	if err != nil {
		handleError(writer, request, err)
		return
	}
	respond.JSON(writer, http.StatusOK, batch)
}

func (handlers *Handlers) CancelBatch(writer http.ResponseWriter, request *http.Request) {
	if err := handlers.service.CancelBatch(request.Context(), chi.URLParam(request, "batchId")); err != nil {
		handleError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (handlers *Handlers) ConfirmBatch(writer http.ResponseWriter, request *http.Request) {
	var confirmation BatchConfirmation
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, MAX_BATCH_CONFIRMATION_BODY_BYTES))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&confirmation); err != nil || confirmation.Revision < 1 || confirmation.SelectedFileIDs == nil {
		respond.Error(writer, http.StatusBadRequest, "invalid_batch_confirmation", "Managed Import Batch confirmation requires a positive revision and selected file IDs")
		return
	}
	batch, err := handlers.service.ConfirmBatch(request.Context(), chi.URLParam(request, "batchId"), confirmation)
	if err != nil {
		handleError(writer, request, err)
		return
	}
	respond.JSON(writer, http.StatusOK, batch)
}

func (handlers *Handlers) GetJob(writer http.ResponseWriter, request *http.Request) {
	job, err := handlers.service.GetJob(request.Context(), chi.URLParam(request, "importId"))
	if err != nil {
		handleError(writer, request, err)
		return
	}
	respond.JSON(writer, http.StatusOK, job)
}

func (handlers *Handlers) PreviewMigration(writer http.ResponseWriter, request *http.Request) {
	if request.Header.Get(MIGRATION_PREVIEW_REQUEST_HEADER) != "1" {
		respond.Error(writer, http.StatusForbidden, "migration_preview_forbidden", "Library Migration preview requires an explicit application request")
		return
	}
	preview, err := handlers.service.PreviewMigration(request.Context())
	if err != nil {
		handleError(writer, request, err)
		return
	}
	respond.JSON(writer, http.StatusOK, preview)
}

func (handlers *Handlers) CancelJob(writer http.ResponseWriter, request *http.Request) {
	if err := handlers.service.CancelJob(request.Context(), chi.URLParam(request, "importId")); err != nil {
		handleError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (handlers *Handlers) UploadFile(writer http.ResponseWriter, request *http.Request) {
	preview, err := handlers.service.Upload(
		request.Context(),
		chi.URLParam(request, "importId"),
		request.Header.Get("X-Import-Filename"),
		request.Body,
		request.ContentLength,
	)
	if err != nil {
		handleError(writer, request, err)
		return
	}
	respond.JSON(writer, http.StatusOK, preview)
}

func (handlers *Handlers) Confirm(writer http.ResponseWriter, request *http.Request) {
	var confirmation Confirmation
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, MAX_CONFIRMATION_BODY_BYTES))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&confirmation); err != nil || confirmation.Revision < 1 {
		respond.Error(writer, http.StatusBadRequest, "invalid_confirmation", "Managed Import confirmation requires a positive revision")
		return
	}
	result, err := handlers.service.Confirm(request.Context(), chi.URLParam(request, "importId"), confirmation.Revision)
	if err != nil {
		handleError(writer, request, err)
		return
	}
	respond.JSON(writer, http.StatusOK, result)
}

func handleError(writer http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		respond.Error(writer, http.StatusNotFound, "import_not_found", "Managed Import Job not found")
	case errors.Is(err, ErrRevisionConflict):
		respond.Error(writer, http.StatusConflict, ERROR_CODE_REVISION_CONFLICT, "Import Preview changed since the supplied revision")
	case errors.Is(err, ErrExactDuplicate):
		respond.Error(writer, http.StatusConflict, ERROR_CODE_EXACT_DUPLICATE, "File bytes match an existing Track")
	case errors.Is(err, ErrBatchTooLarge):
		respond.Error(writer, http.StatusRequestEntityTooLarge, "batch_upload_too_large", "Managed Import batch exceeds the configured byte limit")
	case errors.Is(err, ErrUploadInterrupted):
		respond.Error(writer, http.StatusRequestTimeout, UPLOAD_INTERRUPTED_ERROR_CODE, "Managed Import upload was interrupted; retry this file")
	case errors.Is(err, ErrInvalidState):
		respond.Error(writer, http.StatusConflict, "import_state_conflict", "Managed Import Job is not awaiting this operation")
	case errors.Is(err, ErrUploadTooLarge):
		respond.Error(writer, http.StatusRequestEntityTooLarge, "upload_too_large", "Managed Import file exceeds the configured per-file byte limit")
	case errors.Is(err, ErrInsufficientStorage):
		respond.Error(writer, http.StatusInsufficientStorage, "insufficient_storage", "Managed Storage does not have enough capacity for this import and its safety reserve")
	case errors.Is(err, ErrUnsafeStoragePath):
		respond.Error(writer, http.StatusConflict, "unsafe_storage_path", "Managed Storage path failed containment checks")
	case errors.Is(err, ErrMigrationInProgress):
		respond.Error(writer, http.StatusConflict, "migration_preview_in_progress", "A Library Migration preview is already in progress")
	case errors.Is(err, ErrInvalidUpload):
		respond.Error(writer, http.StatusBadRequest, "invalid_upload", err.Error())
	default:
		var validationErr *ValidationError
		if errors.As(err, &validationErr) {
			reason := strictValidationReason(validationErr)
			respond.ErrorWithField(writer, http.StatusUnprocessableEntity, validationErr.Code, strictValidationMessage(validationErr, reason), validationErr.Field, reason)
			return
		}
		slog.ErrorContext(request.Context(), "Managed Import request failed", "path", request.URL.Path, "error", err)
		respond.Error(writer, http.StatusInternalServerError, "internal_error", "Managed Import request failed")
	}
}

func strictValidationMessage(validationErr *ValidationError, reason string) string {
	if validationErr.Code == string(library.INSPECTION_ERROR_MISSING_ARTWORK) {
		return "Embedded front-cover artwork is required; add one with MusicBrainz Picard and retry"
	}
	if validationErr.Field == "" {
		return fmt.Sprintf("File failed the Strict Import Profile: %s", reason)
	}
	return fmt.Sprintf("File failed the Strict Import Profile at %s: %s", validationErr.Field, reason)
}

func strictValidationReason(validationErr *ValidationError) string {
	if validationErr.Reason == "" {
		return "validation failed"
	}
	return validationErr.Reason
}

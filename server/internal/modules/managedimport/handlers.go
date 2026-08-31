package managedimport

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"

	"github.com/ardam/navidrome-replacement/server/internal/api/respond"
	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	service *Service
}

func NewHandlers(service *Service) *Handlers {
	return &Handlers{service: service}
}

func (handlers *Handlers) CreateJob(writer http.ResponseWriter, request *http.Request) {
	job, err := handlers.service.CreateJob(request.Context())
	if err != nil {
		handleError(writer, request, err)
		return
	}
	respond.JSON(writer, http.StatusCreated, job)
}

func (handlers *Handlers) UploadFile(writer http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "audio/flac" {
		respond.Error(writer, http.StatusBadRequest, "invalid_content_type", "Managed Import requires Content-Type audio/flac")
		return
	}
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
		respond.Error(writer, http.StatusConflict, "import_revision_conflict", "Import Preview changed since the supplied revision")
	case errors.Is(err, ErrInvalidState):
		respond.Error(writer, http.StatusConflict, "import_state_conflict", "Managed Import Job is not awaiting this operation")
	case errors.Is(err, ErrUploadTooLarge):
		respond.Error(writer, http.StatusRequestEntityTooLarge, "upload_too_large", fmt.Sprintf("Managed Import file exceeds the %d byte limit", MAX_UPLOAD_SIZE_BYTES))
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

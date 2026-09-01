package managedimport

import (
	"database/sql"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/go-chi/chi/v5"
)

type Module struct {
	handlers *Handlers
}

func NewModule(database *sql.DB, configuration config.Config, inspector library.MediaInspector) *Module {
	return newModule(database, configuration, inspector, availableStorageBytes)
}

func newModule(database *sql.DB, configuration config.Config, inspector library.MediaInspector, capacity storageCapacity) *Module {
	store := NewStore(database)
	fileLimit := configuration.ManagedImportFileLimitBytes
	if fileLimit <= 0 {
		fileLimit = config.DEFAULT_MANAGED_IMPORT_FILE_LIMIT_BYTES
	}
	batchLimit := configuration.ManagedImportBatchLimitBytes
	if batchLimit <= 0 {
		batchLimit = config.DEFAULT_MANAGED_IMPORT_BATCH_LIMIT_BYTES
	}
	storage := newStorage(configuration.ManagedStoragePath, StorageLimits{
		ReserveBytes: configuration.ManagedStorageReserveBytes,
		FileBytes:    fileLimit,
		BatchBytes:   batchLimit,
	}, capacity)
	service := NewService(store, storage, inspector)
	return &Module{handlers: NewHandlers(service)}
}

func (module *Module) Name() string {
	return "managed-import"
}

func (module *Module) RegisterRoutes(router chi.Router) {
	router.Post("/api/v1/import-batches", module.handlers.CreateBatch)
	router.Get("/api/v1/import-batches/{batchId}", module.handlers.GetBatch)
	router.Post("/api/v1/import-batches/{batchId}/confirm", module.handlers.ConfirmBatch)
	router.Post("/api/v1/imports", module.handlers.CreateJob)
	router.Get("/api/v1/imports/{importId}", module.handlers.GetJob)
	router.Put("/api/v1/imports/{importId}/file", module.handlers.UploadFile)
	router.Post("/api/v1/imports/{importId}/confirm", module.handlers.Confirm)
}

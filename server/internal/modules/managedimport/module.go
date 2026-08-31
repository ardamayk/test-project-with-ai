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
	store := NewStore(database)
	storage := NewStorage(configuration.ManagedStoragePath)
	service := NewService(store, storage, inspector)
	return &Module{handlers: NewHandlers(service)}
}

func (module *Module) Name() string {
	return "managed-import"
}

func (module *Module) RegisterRoutes(router chi.Router) {
	router.Post("/api/v1/imports", module.handlers.CreateJob)
	router.Put("/api/v1/imports/{importId}/file", module.handlers.UploadFile)
	router.Post("/api/v1/imports/{importId}/confirm", module.handlers.Confirm)
}

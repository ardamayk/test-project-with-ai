package library

import (
	"context"
	"database/sql"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/go-chi/chi/v5"
)

type Module struct {
	handlers *Handlers
	service  *Service
	store    *Store
}

func NewModule(db *sql.DB, cfg config.Config) *Module {
	store := NewStore(db)
	service := NewService(store, cfg)
	return &Module{
		handlers: NewHandlers(service),
		service:  service,
		store:    store,
	}
}

func (m *Module) Name() string {
	return "library"
}

func (m *Module) Start(ctx context.Context) error {
	if err := m.store.RecoverInterruptedScans(ctx); err != nil {
		return err
	}
	if !m.service.MusicPathsConfigured() {
		return nil
	}
	_, err := m.service.TriggerScan(ctx)
	return err
}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Post("/api/v1/library/scan", m.handlers.TriggerScan)
	r.Get("/api/v1/library/scan/status", m.handlers.GetScanStatus)
	r.Get("/api/v1/library/artists", m.handlers.ListArtists)
	r.Get("/api/v1/library/albums", m.handlers.ListAlbums)
	r.Get("/api/v1/library/albums/{albumId}/cover", m.handlers.GetAlbumCover)
	r.Get("/api/v1/library/albums/{albumId}", m.handlers.GetAlbum)
	r.Delete("/api/v1/library/albums/{albumId}", m.handlers.DeleteAlbum)
	r.Get("/api/v1/library/tracks", m.handlers.ListTracks)
	r.Get("/api/v1/library/tracks/{trackId}", m.handlers.GetTrack)
}

func (m *Module) TrackAccess() TrackAccess {
	return m.service
}

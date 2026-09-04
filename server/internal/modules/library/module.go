package library

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
)

type Module struct {
	handlers *Handlers
	service  *Service
	store    *Store
}

// NewModule wires library reads. Managed Import is the only ingestion path.
func NewModule(db *sql.DB) *Module {
	store := NewStore(db)
	service := NewService(store)
	return &Module{
		handlers: NewHandlers(service),
		service:  service,
		store:    store,
	}
}

func (m *Module) Name() string {
	return "library"
}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/library/artists", m.handlers.ListArtists)
	r.Get("/api/v1/library/albums", m.handlers.ListAlbums)
	r.Get("/api/v1/library/albums/{albumId}/cover", m.handlers.GetAlbumCover)
	r.Get("/api/v1/library/albums/{albumId}", m.handlers.GetAlbum)
	r.Get("/api/v1/library/tracks", m.handlers.ListTracks)
	r.Get("/api/v1/library/tracks/{trackId}", m.handlers.GetTrack)
}

func (m *Module) TrackAccess() TrackAccess {
	return m.service
}

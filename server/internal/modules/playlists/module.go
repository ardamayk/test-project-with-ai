package playlists

import (
	"database/sql"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/go-chi/chi/v5"
)

type Module struct {
	handlers *Handlers
}

func NewModule(db *sql.DB, libStore *library.Store) *Module {
	store := NewStore(db, libStore)
	return &Module{handlers: NewHandlers(store)}
}

func (m *Module) Name() string {
	return "playlists"
}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/playlists", m.handlers.ListPlaylists)
	r.Post("/api/v1/playlists", m.handlers.CreatePlaylist)
	r.Get("/api/v1/playlists/{playlistId}", m.handlers.GetPlaylist)
	r.Post("/api/v1/playlists/{playlistId}/tracks", m.handlers.AddTrack)
	r.Delete("/api/v1/playlists/{playlistId}/tracks/{trackId}", m.handlers.RemoveTrack)
}

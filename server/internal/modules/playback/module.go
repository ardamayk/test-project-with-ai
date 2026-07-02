package playback

import (
	"database/sql"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/go-chi/chi/v5"
)

type Module struct {
	handlers *Handlers
}

func NewModule(db *sql.DB, tracks library.TrackAccess) *Module {
	store := NewStore(db, tracks)
	return &Module{handlers: NewHandlers(store, tracks)}
}

func (m *Module) Name() string {
	return "playback"
}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/playback/queue", m.handlers.GetQueue)
	r.Put("/api/v1/playback/queue", m.handlers.ReplaceQueue)
	r.Post("/api/v1/playback/queue/items", m.handlers.AppendItem)
	r.Delete("/api/v1/playback/queue/items/{itemId}", m.handlers.RemoveItem)
	r.Get("/api/v1/tracks/{trackId}/stream", m.handlers.StreamTrack)
}

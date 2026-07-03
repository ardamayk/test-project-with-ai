package radio

import (
	"database/sql"
	"net/http"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/go-chi/chi/v5"
)

type Module struct {
	handlers *Handlers
}

func NewModule(db *sql.DB, cfg config.Config) *Module {
	store := NewStore(db)
	cache := NewNowPlayingCache()
	searcher := NewRadioBrowserClient(cfg.RadioBrowserBaseURL, http.DefaultClient)
	streamer := NewStreamProxy(store, cache)
	return &Module{handlers: NewHandlers(store, searcher, streamer, cache)}
}

func (m *Module) Name() string {
	return "radio"
}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/radio/stations", m.handlers.ListStations)
	r.Post("/api/v1/radio/stations", m.handlers.CreateStation)
	r.Get("/api/v1/radio/stations/{stationId}", m.handlers.GetStation)
	r.Patch("/api/v1/radio/stations/{stationId}", m.handlers.PatchStation)
	r.Delete("/api/v1/radio/stations/{stationId}", m.handlers.DeleteStation)
	r.Get("/api/v1/radio/search", m.handlers.SearchStations)
	r.Post("/api/v1/radio/import", m.handlers.ImportStation)
	r.Get("/api/v1/radio/stations/{stationId}/stream", m.handlers.StreamStation)
	r.Get("/api/v1/radio/stations/{stationId}/now-playing", m.handlers.GetNowPlaying)
}

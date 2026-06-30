package preferences

import "github.com/go-chi/chi/v5"

type Module struct {
	store *Store
}

func NewModule(store *Store) *Module {
	return &Module{store: store}
}

func (m *Module) Name() string {
	return "preferences"
}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/preferences", m.handleGet)
	r.Patch("/api/v1/preferences", m.handlePatch)
}

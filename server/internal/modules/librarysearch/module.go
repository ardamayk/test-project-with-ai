// Package librarysearch evaluates one coherent Library Search behind the
// Music Server HTTP boundary. It consumes the prepared representations of the
// searchdata package and owns matching, ranking, typo fallback, related
// results, and Best Match selection.
package librarysearch

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
)

type Module struct {
	handlers *Handlers
}

func NewModule(database *sql.DB) *Module {
	return &Module{handlers: NewHandlers(NewIndexLoader(database))}
}

func (m *Module) Name() string {
	return "librarysearch"
}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/library/search", m.handlers.Search)
}

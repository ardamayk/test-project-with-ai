package docs

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/ardam/navidrome-replacement/server/internal/staticassets"
	"github.com/go-chi/chi/v5"
)

type Module struct {
	openAPIPath string
}

func NewModule(openAPIPath string) *Module {
	return &Module{openAPIPath: openAPIPath}
}

func DefaultOpenAPIPath() string {
	path := filepath.Join("..", "packages", "contracts", "openapi.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return filepath.Join("packages", "contracts", "openapi.yaml")
	}
	return path
}

func (m *Module) Name() string {
	return "docs"
}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/api/openapi.yaml", m.ServeOpenAPI)
	r.Get("/api/docs", SwaggerUIHandler())
	r.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/", http.StatusMovedPermanently)
	})
	r.Handle("/docs/*", staticassets.DocsHandler())
}

func (m *Module) ServeOpenAPI(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, m.openAPIPath)
}

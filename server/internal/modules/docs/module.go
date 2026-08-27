package docs

import (
	"encoding/json"
	"net/http"

	apigen "github.com/ardam/navidrome-replacement/server/internal/api/gen"
	"github.com/ardam/navidrome-replacement/server/internal/staticassets"
	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

type Module struct {
	openAPISpec func() ([]byte, error)
}

func NewModule() *Module {
	return NewModuleWithOpenAPISpec(generatedOpenAPISpec)
}

func NewModuleWithOpenAPISpec(openAPISpec func() ([]byte, error)) *Module {
	return &Module{openAPISpec: openAPISpec}
}

func generatedOpenAPISpec() ([]byte, error) {
	const specPath = "openapi.yaml"
	raw, err := apigen.PathToRawSpec(specPath)[specPath]()
	if err != nil {
		return nil, err
	}
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		return nil, err
	}
	return yaml.Marshal(spec)
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
	spec, err := m.openAPISpec()
	if err != nil {
		http.Error(w, "openapi spec unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(spec)
}

package modules

import "github.com/go-chi/chi/v5"

type Module interface {
	Name() string
	RegisterRoutes(r chi.Router)
}

type Registry struct {
	modules []Module
}

func NewRegistry(modules ...Module) *Registry {
	return &Registry{modules: modules}
}

func (r *Registry) RegisterAll(router chi.Router) {
	for _, m := range r.modules {
		m.RegisterRoutes(router)
	}
}

func (r *Registry) Names() []string {
	names := make([]string, len(r.modules))
	for i, m := range r.modules {
		names[i] = m.Name()
	}
	return names
}

package docs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestModuleRegistersDocumentationRoutes(t *testing.T) {
	r := chi.NewRouter()
	NewModuleWithOpenAPISpec(func() ([]byte, error) {
		return []byte("openapi: 3.0.3\ninfo:\n  title: Test API\n"), nil
	}).RegisterRoutes(r)

	tests := []struct {
		path string
		code int
		want string
	}{
		{path: "/api/openapi.yaml", code: http.StatusOK, want: "title: Test API"},
		{path: "/api/docs", code: http.StatusOK, want: "SwaggerUIBundle"},
		{path: "/docs/", code: http.StatusOK, want: "Navidrome Replacement"},
		{path: "/docs", code: http.StatusMovedPermanently, want: "/docs/"},
	}

	for _, tc := range tests {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)

		if rec.Code != tc.code {
			t.Fatalf("%s status = %d, want %d", tc.path, rec.Code, tc.code)
		}
		body := rec.Body.String() + rec.Header().Get("Location")
		if !strings.Contains(body, tc.want) {
			t.Fatalf("%s body missing %q", tc.path, tc.want)
		}
	}
}

func TestModuleServesGeneratedOpenAPI(t *testing.T) {
	r := chi.NewRouter()
	NewModule().RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/openapi.yaml", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/yaml" {
		t.Fatalf("Content-Type = %q, want application/yaml", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, "openapi: 3.0.3") {
		t.Fatalf("body missing generated OpenAPI spec")
	}
}

func TestModuleName(t *testing.T) {
	if got := NewModule().Name(); got != "docs" {
		t.Fatalf("Name() = %q, want docs", got)
	}
}

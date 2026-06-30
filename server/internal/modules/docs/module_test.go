package docs

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestModuleRegistersDocumentationRoutes(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "openapi.yaml")
	if err := os.WriteFile(specPath, []byte("openapi: 3.0.3\ninfo:\n  title: Test API\n"), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	r := chi.NewRouter()
	NewModule(specPath).RegisterRoutes(r)

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

func TestModuleName(t *testing.T) {
	if got := NewModule("openapi.yaml").Name(); got != "docs" {
		t.Fatalf("Name() = %q, want docs", got)
	}
}

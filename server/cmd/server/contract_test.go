package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/api"
	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

const CONTRACT_TEST_ORIGIN = "https://app.example"

var routeParameterPattern = regexp.MustCompile(`\{[^}]+\}`)

func newContractServer(t *testing.T) chi.Router {
	t.Helper()
	cfg := config.Config{
		Version:            "contract-test",
		CORSOrigins:        []string{CONTRACT_TEST_ORIGIN},
		ManagedStoragePath: t.TempDir(),
		MusicPaths:         []string{t.TempDir()},
	}
	return newAssembledServer(cfg, testutil.OpenMigratedDB(t)).router
}

// normalizeRoutePattern makes chi patterns and OpenAPI path templates
// comparable regardless of how each names its path parameters.
func normalizeRoutePattern(pattern string) string {
	return routeParameterPattern.ReplaceAllString(pattern, "{}")
}

func TestAssembledServerMountsExactlyTheDocumentedOperations(t *testing.T) {
	documented := map[string]string{}
	for path, item := range testutil.Contract(t).Paths.Map() {
		for method, operation := range item.Operations() {
			documented[method+" "+normalizeRoutePattern(path)] = operation.OperationID
		}
	}

	mounted := map[string]bool{}
	walkErr := chi.Walk(newContractServer(t), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/api/v1/") {
			mounted[method+" "+normalizeRoutePattern(route)] = true
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk assembled router: %v", walkErr)
	}

	for key, operationID := range documented {
		if !mounted[key] {
			t.Errorf("documented operation %s (%s) is not mounted on the assembled server", operationID, key)
		}
	}
	for key := range mounted {
		if _, ok := documented[key]; !ok {
			t.Errorf("mounted route %s is not documented in packages/contracts/openapi.yaml", key)
		}
	}
}

func TestAssembledServerAdvertisesOnlyDocumentedCapabilities(t *testing.T) {
	server := newContractServer(t)
	response := testutil.ServeContractRequest(t, server, testutil.ContractRequest{Method: http.MethodGet, Path: "/api/v1/health"})
	var health struct {
		Status       string   `json:"status"`
		Capabilities []string `json:"capabilities"`
	}
	testutil.DecodeJSON(t, response, &health)

	if !slices.Equal(health.Capabilities, api.ServerCapabilities()) {
		t.Fatalf("advertised capabilities = %v, want %v", health.Capabilities, api.ServerCapabilities())
	}
	documentation := testutil.Contract(t).Components.Schemas["HealthResponse"].Value.Properties["capabilities"].Value.Description
	for _, capability := range health.Capabilities {
		if !strings.Contains(documentation, capability) {
			t.Errorf("capability %q is advertised but not documented in HealthResponse.capabilities", capability)
		}
	}
	for _, required := range []string{"managed-import.v1", "managed-import-batches.v1", "library-migration.v1", "managed-track-deletion.v1", "managed-track-replacement.v1"} {
		if !slices.Contains(health.Capabilities, required) {
			t.Errorf("capability %q is missing from the health response", required)
		}
	}
}

func TestAssembledServerKeepsDeprecatedLegacyScanBehaviorForOlderClients(t *testing.T) {
	contract := testutil.Contract(t)
	if trigger := contract.Paths.Find("/api/v1/library/scan"); trigger == nil || trigger.Post == nil || !trigger.Post.Deprecated {
		t.Fatal("POST /api/v1/library/scan must stay documented and marked deprecated")
	}
	if status := contract.Paths.Find("/api/v1/library/scan/status"); status == nil || status.Get == nil || !status.Get.Deprecated {
		t.Fatal("GET /api/v1/library/scan/status must stay documented and marked deprecated")
	}

	server := newContractServer(t)
	trigger := testutil.ServeContractRequest(t, server, testutil.ContractRequest{Method: http.MethodPost, Path: "/api/v1/library/scan"})
	testutil.AssertStructuredError(t, trigger, http.StatusGone, "legacy_scan_retired")
	if trigger.Header().Get("Deprecation") == "" {
		t.Fatal("retired scan trigger must carry a Deprecation header")
	}

	status := testutil.ServeContractRequest(t, server, testutil.ContractRequest{Method: http.MethodGet, Path: "/api/v1/library/scan/status"})
	var scanStatus struct {
		Status  string `json:"status"`
		Scanned int    `json:"scanned"`
		Added   int    `json:"added"`
	}
	testutil.DecodeJSON(t, status, &scanStatus)
	if status.Code != http.StatusOK || scanStatus.Status != "idle" || scanStatus.Scanned != 0 || scanStatus.Added != 0 {
		t.Fatalf("legacy scan status = %d %+v, want 200 idle with zero counts", status.Code, scanStatus)
	}
}

func TestCORSAllowsEveryDocumentedRequestHeader(t *testing.T) {
	headerNames := map[string]bool{}
	for _, item := range testutil.Contract(t).Paths.Map() {
		for _, parameter := range item.Parameters {
			if parameter.Value.In == "header" {
				headerNames[parameter.Value.Name] = true
			}
		}
		for _, operation := range item.Operations() {
			for _, parameter := range operation.Parameters {
				if parameter.Value.In == "header" {
					headerNames[parameter.Value.Name] = true
				}
			}
		}
	}
	if len(headerNames) == 0 {
		t.Fatal("contract documents no request headers")
	}
	names := make([]string, 0, len(headerNames))
	for name := range headerNames {
		names = append(names, name)
	}
	sort.Strings(names)

	handler := corsHandler([]string{CONTRACT_TEST_ORIGIN})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, name := range names {
		request := httptest.NewRequest(http.MethodOptions, "/api/v1/contract", nil)
		request.Header.Set("Origin", CONTRACT_TEST_ORIGIN)
		request.Header.Set("Access-Control-Request-Method", http.MethodPost)
		request.Header.Set("Access-Control-Request-Headers", name)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		allowed := strings.ToLower(response.Header().Get("Access-Control-Allow-Headers"))
		if response.Code != http.StatusOK || !strings.Contains(allowed, strings.ToLower(name)) {
			t.Errorf("documented header %s is not allowed by CORS preflight (status %d, allow = %q)", name, response.Code, allowed)
		}
	}
}

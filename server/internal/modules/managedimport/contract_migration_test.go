package managedimport

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

// TestLibraryMigrationContractCoversEveryPhase validates the explicit Library
// Migration workflow (preview, stage, cutover, Legacy Source Cleanup) and its
// confirmation-header guards against the embedded OpenAPI contract.
func TestLibraryMigrationContractCoversEveryPhase(t *testing.T) {
	fixture := newCleanupFixture(t)
	router := fixture.router

	previewForbidden := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodPost, Path: "/api/v1/library-migrations/preview"})
	testutil.AssertStructuredError(t, previewForbidden, http.StatusForbidden, "migration_preview_forbidden")
	preview := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodPost, Path: "/api/v1/library-migrations/preview", Headers: map[string]string{MIGRATION_PREVIEW_REQUEST_HEADER: "1"}})
	var migrationPreview MigrationPreview
	testutil.DecodeJSON(t, preview, &migrationPreview)
	if preview.Code != http.StatusOK || migrationPreview.AcceptedCount != 2 || migrationPreview.RejectedCount != 1 {
		t.Fatalf("migration preview = %d %+v, want 2 accepted and 1 rejected", preview.Code, migrationPreview)
	}

	stageForbidden := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodPost, Path: "/api/v1/library-migrations/stage"})
	testutil.AssertStructuredError(t, stageForbidden, http.StatusForbidden, "migration_stage_forbidden")
	stage := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodPost, Path: "/api/v1/library-migrations/stage", Headers: map[string]string{MIGRATION_STAGE_REQUEST_HEADER: "1"}})
	if stage.Code != http.StatusOK {
		t.Fatalf("migration stage status = %d, body = %s", stage.Code, stage.Body.String())
	}

	cutoverForbidden := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodPost, Path: "/api/v1/library-migrations/cutover"})
	testutil.AssertStructuredError(t, cutoverForbidden, http.StatusForbidden, "migration_cutover_forbidden")
	cutoverResponse := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodPost, Path: "/api/v1/library-migrations/cutover", Headers: map[string]string{MIGRATION_CUTOVER_REQUEST_HEADER: "1"}})
	var cutover MigrationCutover
	testutil.DecodeJSON(t, cutoverResponse, &cutover)
	if cutoverResponse.Code != http.StatusOK || cutover.MigratedCount != 2 || cutover.RejectedCount != 1 {
		t.Fatalf("migration cutover = %d %+v, want 2 migrated and 1 rejected", cutoverResponse.Code, cutover)
	}
	firstTrackID := findCutoverFile(t, cutover, fixture.firstLegacyID).CreatedTrackID

	cleanupPreview := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodGet, Path: "/api/v1/library-migrations/cleanup"})
	var eligible MigrationCleanupPreview
	testutil.DecodeJSON(t, cleanupPreview, &eligible)
	if cleanupPreview.Code != http.StatusOK || eligible.EligibleCount != 2 {
		t.Fatalf("cleanup preview = %d %+v, want 2 eligible sources", cleanupPreview.Code, eligible)
	}

	confirmation, err := json.Marshal(MigrationCleanupConfirmation{TrackIDs: []string{firstTrackID}, FileCount: 1, TotalSizeBytes: int64(len(fixture.firstContents))})
	if err != nil {
		t.Fatalf("encode cleanup confirmation: %v", err)
	}
	cleanupForbidden := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodPost, Path: "/api/v1/library-migrations/cleanup", Body: confirmation, Headers: map[string]string{"Content-Type": "application/json"}})
	testutil.AssertStructuredError(t, cleanupForbidden, http.StatusForbidden, "migration_cleanup_forbidden")
	malformed := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodPost, Path: "/api/v1/library-migrations/cleanup", Body: []byte(`{"trackIds":[]}`), Headers: map[string]string{"Content-Type": "application/json", MIGRATION_CLEANUP_REQUEST_HEADER: "1"}})
	testutil.AssertStructuredError(t, malformed, http.StatusBadRequest, "invalid_cleanup_confirmation")
	cleanupResponse := testutil.ServeContractRequest(t, router, testutil.ContractRequest{Method: http.MethodPost, Path: "/api/v1/library-migrations/cleanup", Body: confirmation, Headers: map[string]string{"Content-Type": "application/json", MIGRATION_CLEANUP_REQUEST_HEADER: "1"}})
	var cleanup MigrationCleanup
	testutil.DecodeJSON(t, cleanupResponse, &cleanup)
	if cleanupResponse.Code != http.StatusOK || cleanup.DeletedCount != 1 || cleanup.FailedCount != 0 {
		t.Fatalf("legacy source cleanup = %d %+v, want exactly one deleted file", cleanupResponse.Code, cleanup)
	}
	fixture.assertSourcesIntact(t, fixture.secondPath, fixture.rejectedPath)
}

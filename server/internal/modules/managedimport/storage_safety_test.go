package managedimport

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

func TestManagedImportRejectsUploadWhenStorageReserveWouldBeExhausted(t *testing.T) {
	fixture := readStorageSafetyFixture(t)
	const reserveBytes int64 = 1024
	availableBytes := reserveBytes + int64(len(fixture)) - 1
	router := newStorageSafetyRouter(t, config.Config{
		ManagedStoragePath:           t.TempDir(),
		ManagedStorageReserveBytes:   reserveBytes,
		ManagedImportFileLimitBytes:  int64(len(fixture) * 2),
		ManagedImportBatchLimitBytes: int64(len(fixture) * 2),
	}, func(string) (int64, error) {
		return availableBytes, nil
	})
	jobID := createStorageSafetyJob(t, router)

	response := serveStorageSafetyRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(fixture), map[string]string{
		"Content-Type":      "audio/flac",
		"X-Import-Filename": "strict-import.flac",
	})

	assertStorageSafetyError(t, response, http.StatusInsufficientStorage, "insufficient_storage")
}

func TestManagedImportRechecksSelectedAndTemporaryBytesBeforeCommit(t *testing.T) {
	fixturePath := filepath.Join("..", "library", "testdata", "strict-import.flac")
	fixture := readStorageSafetyFixture(t)
	inspection, err := library.NewMediaInspector().Inspect(fixturePath)
	if err != nil {
		t.Fatalf("inspect fixture: %v", err)
	}
	const reserveBytes int64 = 1024
	availableBytes := int64(1 << 40)
	managedStoragePath := t.TempDir()
	router := newStorageSafetyRouter(t, config.Config{
		ManagedStoragePath:           managedStoragePath,
		ManagedStorageReserveBytes:   reserveBytes,
		ManagedImportFileLimitBytes:  int64(len(fixture) * 2),
		ManagedImportBatchLimitBytes: int64(len(fixture) * 2),
	}, func(string) (int64, error) {
		return availableBytes, nil
	})
	jobID := createStorageSafetyJob(t, router)
	uploadResponse := serveStorageSafetyRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(fixture), map[string]string{
		"Content-Type":      "audio/flac",
		"X-Import-Filename": "strict-import.flac",
	})
	if uploadResponse.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body = %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	var preview struct {
		Revision int `json:"revision"`
	}
	decodeStorageSafetyJSON(t, uploadResponse, &preview)
	availableBytes = reserveBytes + int64(len(fixture)) + int64(len(inspection.AlbumArtwork.Data)) - 1

	response := serveStorageSafetyRequest(t, router, http.MethodPost, "/api/v1/imports/"+jobID+"/confirm", strings.NewReader(`{"revision":2}`), map[string]string{
		"Content-Type": "application/json",
	})

	assertStorageSafetyError(t, response, http.StatusInsufficientStorage, "insufficient_storage")
	assertNoCanonicalAudio(t, managedStoragePath)
}

func TestManagedImportRejectsStagingSymlinkEscape(t *testing.T) {
	managedStoragePath := t.TempDir()
	outsidePath := t.TempDir()
	if err := os.Symlink(outsidePath, filepath.Join(managedStoragePath, ".staging")); err != nil {
		t.Fatalf("create staging symlink: %v", err)
	}
	router := newStorageSafetyRouter(t, config.Config{
		ManagedStoragePath: managedStoragePath,
	}, unlimitedStorageCapacity)
	jobID := createStorageSafetyJob(t, router)

	response := serveStorageSafetyRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(readStorageSafetyFixture(t)), map[string]string{
		"Content-Type":      "audio/flac",
		"X-Import-Filename": "strict-import.flac",
	})

	assertStorageSafetyError(t, response, http.StatusConflict, "unsafe_storage_path")
	assertDirectoryEmpty(t, outsidePath)
}

func TestManagedImportRejectsCanonicalLibrarySymlinkEscape(t *testing.T) {
	managedStoragePath := t.TempDir()
	outsidePath := t.TempDir()
	if err := os.Symlink(outsidePath, filepath.Join(managedStoragePath, "library")); err != nil {
		t.Fatalf("create library symlink: %v", err)
	}
	router := newStorageSafetyRouter(t, config.Config{
		ManagedStoragePath: managedStoragePath,
	}, unlimitedStorageCapacity)
	jobID := createStorageSafetyJob(t, router)
	uploadResponse := serveStorageSafetyRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(readStorageSafetyFixture(t)), map[string]string{
		"Content-Type":      "audio/flac",
		"X-Import-Filename": "strict-import.flac",
	})
	if uploadResponse.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body = %s", uploadResponse.Code, uploadResponse.Body.String())
	}

	response := serveStorageSafetyRequest(t, router, http.MethodPost, "/api/v1/imports/"+jobID+"/confirm", strings.NewReader(`{"revision":2}`), map[string]string{
		"Content-Type": "application/json",
	})

	assertStorageSafetyError(t, response, http.StatusConflict, "unsafe_storage_path")
	assertDirectoryEmpty(t, outsidePath)
}

func newStorageSafetyRouter(t *testing.T, configuration config.Config, capacity storageCapacity) http.Handler {
	t.Helper()
	database := testutil.OpenMigratedDB(t)
	module := newModule(database, configuration, library.NewMediaInspector(), capacity)
	router := chi.NewRouter()
	module.RegisterRoutes(router)
	return router
}

func createStorageSafetyJob(t *testing.T, router http.Handler) string {
	t.Helper()
	response := serveStorageSafetyRequest(t, router, http.MethodPost, "/api/v1/imports", nil, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create Import Job status = %d, body = %s", response.Code, response.Body.String())
	}
	var job struct {
		ID string `json:"id"`
	}
	decodeStorageSafetyJSON(t, response, &job)
	return job.ID
}

func serveStorageSafetyRequest(t *testing.T, router http.Handler, method, path string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, body)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func assertStorageSafetyError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d, body = %s", response.Code, status, response.Body.String())
	}
	var failure struct {
		Code string `json:"code"`
	}
	decodeStorageSafetyJSON(t, response, &failure)
	if failure.Code != code {
		t.Fatalf("error code = %q, want %q", failure.Code, code)
	}
}

func decodeStorageSafetyJSON(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func assertNoCanonicalAudio(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Ext(path) == ".flac" {
			t.Fatalf("unexpected canonical audio at %q", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Managed Storage: %v", err)
	}
}

func readStorageSafetyFixture(t *testing.T) []byte {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("..", "library", "testdata", "strict-import.flac"))
	if err != nil {
		t.Fatalf("read strict FLAC fixture: %v", err)
	}
	return fixture
}

func unlimitedStorageCapacity(string) (int64, error) {
	return int64(1 << 40), nil
}

func assertDirectoryEmpty(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("read directory %q: %v", path, err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory %q contains escaped files: %v", path, entries)
	}
}

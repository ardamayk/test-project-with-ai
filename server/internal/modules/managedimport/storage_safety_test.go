package managedimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
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
	jobID := testutil.CreateResourceID(t, router, "/api/v1/imports")

	response := testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(fixture), map[string]string{
		"Content-Type":      "audio/flac",
		"X-Import-Filename": "strict-import.flac",
	})

	testutil.AssertErrorCode(t, response, http.StatusInsufficientStorage, "insufficient_storage")
}

func TestFailureDetailsPreservesStorageErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   string
		wantReason string
	}{
		{name: "insufficient storage", err: ErrInsufficientStorage, wantCode: "insufficient_storage", wantReason: "Managed Storage does not have enough capacity for this import and its safety reserve"},
		{name: "unsafe storage path", err: ErrUnsafeStoragePath, wantCode: "unsafe_storage_path", wantReason: "Managed Storage path failed containment checks"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			code, reason := failureDetails(testCase.err)
			if code != testCase.wantCode || reason != testCase.wantReason {
				t.Fatalf("failure details = (%q, %q), want (%q, %q)", code, reason, testCase.wantCode, testCase.wantReason)
			}
		})
	}
}

func TestManagedImportRechecksSelectedAndTemporaryBytesBeforeCommit(t *testing.T) {
	fixturePath := filepath.Join("..", "library", "testdata", "strict-import.flac")
	fixture := readStorageSafetyFixture(t)
	inspection, err := library.NewMediaInspector().Inspect(context.Background(), fixturePath, nil)
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
	jobID := testutil.CreateResourceID(t, router, "/api/v1/imports")
	uploadResponse := testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(fixture), map[string]string{
		"Content-Type":      "audio/flac",
		"X-Import-Filename": "strict-import.flac",
	})
	if uploadResponse.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body = %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	var preview struct {
		Revision int `json:"revision"`
	}
	testutil.DecodeJSON(t, uploadResponse, &preview)
	availableBytes = reserveBytes + int64(len(fixture)) + int64(len(inspection.AlbumArtwork.Data)) - 1

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/imports/"+jobID+"/confirm", strings.NewReader(`{"revision":2}`), map[string]string{
		"Content-Type": "application/json",
	})

	testutil.AssertErrorCode(t, response, http.StatusInsufficientStorage, "insufficient_storage")
	assertNoCanonicalAudio(t, managedStoragePath)
}

func TestManagedImportBatchPreservesConfirmationFailureCode(t *testing.T) {
	fixture := readStorageSafetyFixture(t)
	const reserveBytes int64 = 1024
	availableBytes := int64(1 << 40)
	router := newStorageSafetyRouter(t, config.Config{
		ManagedStoragePath:           t.TempDir(),
		ManagedStorageReserveBytes:   reserveBytes,
		ManagedImportFileLimitBytes:  int64(len(fixture) * 2),
		ManagedImportBatchLimitBytes: int64(len(fixture) * 2),
	}, func(string) (int64, error) {
		return availableBytes, nil
	})
	batchID := testutil.CreateResourceID(t, router, "/api/v1/import-batches")
	jobResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/imports", strings.NewReader(fmt.Sprintf(`{"batchId":%q}`, batchID)), map[string]string{"Content-Type": "application/json"})
	var job Job
	testutil.DecodeJSON(t, jobResponse, &job)
	uploadResponse := testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+job.ID+"/file", bytes.NewReader(fixture), map[string]string{
		"Content-Type": "audio/flac", "X-Import-Filename": "strict-import.flac",
	})
	if uploadResponse.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body = %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	batchResponse := testutil.ServeRequest(t, router, http.MethodGet, "/api/v1/import-batches/"+batchID, nil, nil)
	var batch Batch
	testutil.DecodeJSON(t, batchResponse, &batch)
	availableBytes = reserveBytes

	confirmResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/import-batches/"+batchID+"/confirm", strings.NewReader(fmt.Sprintf(`{"revision":%d,"selectedFileIds":[%q]}`, batch.Revision, job.ID)), map[string]string{"Content-Type": "application/json"})
	if confirmResponse.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, body = %s", confirmResponse.Code, confirmResponse.Body.String())
	}
	testutil.DecodeJSON(t, confirmResponse, &batch)
	if len(batch.Files) != 1 || batch.Files[0].ErrorCode != "insufficient_storage" {
		t.Fatalf("confirmation failure = %+v", batch.Files)
	}
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
	jobID := testutil.CreateResourceID(t, router, "/api/v1/imports")

	response := testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(readStorageSafetyFixture(t)), map[string]string{
		"Content-Type":      "audio/flac",
		"X-Import-Filename": "strict-import.flac",
	})

	testutil.AssertErrorCode(t, response, http.StatusConflict, "unsafe_storage_path")
	assertDirectoryEmpty(t, outsidePath)
}

func TestManagedImportRejectsManagedStorageRootSymlink(t *testing.T) {
	outsidePath := t.TempDir()
	managedStoragePath := filepath.Join(t.TempDir(), "managed")
	if err := os.Symlink(outsidePath, managedStoragePath); err != nil {
		t.Fatalf("create Managed Storage root symlink: %v", err)
	}
	router := newStorageSafetyRouter(t, config.Config{
		ManagedStoragePath: managedStoragePath,
	}, unlimitedStorageCapacity)
	jobID := testutil.CreateResourceID(t, router, "/api/v1/imports")

	response := testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(readStorageSafetyFixture(t)), map[string]string{
		"Content-Type":      "audio/flac",
		"X-Import-Filename": "strict-import.flac",
	})

	testutil.AssertErrorCode(t, response, http.StatusConflict, "unsafe_storage_path")
	assertDirectoryEmpty(t, outsidePath)
}

func TestManagedImportRejectsManagedStorageAncestorSymlink(t *testing.T) {
	outsidePath := t.TempDir()
	ancestorPath := filepath.Join(t.TempDir(), "linked-parent")
	if err := os.Symlink(outsidePath, ancestorPath); err != nil {
		t.Fatalf("create Managed Storage ancestor symlink: %v", err)
	}
	managedStoragePath := filepath.Join(ancestorPath, "managed")
	router := newStorageSafetyRouter(t, config.Config{
		ManagedStoragePath: managedStoragePath,
	}, unlimitedStorageCapacity)
	jobID := testutil.CreateResourceID(t, router, "/api/v1/imports")

	response := testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(readStorageSafetyFixture(t)), map[string]string{
		"Content-Type":      "audio/flac",
		"X-Import-Filename": "strict-import.flac",
	})

	testutil.AssertErrorCode(t, response, http.StatusConflict, "unsafe_storage_path")
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
	jobID := testutil.CreateResourceID(t, router, "/api/v1/imports")
	uploadResponse := testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(readStorageSafetyFixture(t)), map[string]string{
		"Content-Type":      "audio/flac",
		"X-Import-Filename": "strict-import.flac",
	})
	if uploadResponse.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body = %s", uploadResponse.Code, uploadResponse.Body.String())
	}

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/imports/"+jobID+"/confirm", strings.NewReader(`{"revision":2}`), map[string]string{
		"Content-Type": "application/json",
	})

	testutil.AssertErrorCode(t, response, http.StatusConflict, "unsafe_storage_path")
	assertDirectoryEmpty(t, outsidePath)
}

func TestManagedImportRollbackRemovesUncommittedCanonicalArtwork(t *testing.T) {
	fixture := readStorageSafetyFixture(t)
	fixturePath := filepath.Join("..", "library", "testdata", "strict-import.flac")
	inspection, err := library.NewMediaInspector().Inspect(context.Background(), fixturePath, nil)
	if err != nil {
		t.Fatalf("inspect fixture: %v", err)
	}
	storage := newStorage(t.TempDir(), StorageLimits{
		FileBytes:  int64(len(fixture) * 2),
		BatchBytes: int64(len(fixture) * 2),
	}, unlimitedStorageCapacity)
	firstUpload, err := storage.StageUpload(bytes.NewReader(fixture), int64(len(fixture)))
	if err != nil {
		t.Fatalf("stage first Managed Track: %v", err)
	}
	firstPlacement, err := storage.Place(firstUpload.Path, inspection, commitIdentity{
		AlbumArtistID: "album-artist-id",
		AlbumID:       "album-id",
		TrackID:       "first-track-id",
	})
	if err != nil {
		t.Fatalf("place first Managed Track: %v", err)
	}
	if err := storage.Rollback(firstPlacement); err != nil {
		t.Fatalf("rollback uncommitted Managed Track: %v", err)
	}
	if _, err := os.Stat(firstPlacement.ArtworkPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat rolled-back canonical Album Artwork: %v", err)
	}
}

func TestManagedImportPublishesArtworkWithoutReplacingConcurrentWinner(t *testing.T) {
	storage := newStorage(t.TempDir(), StorageLimits{FileBytes: 1024, BatchBytes: 1024}, unlimitedStorageCapacity)
	root, openErr := storage.openRoot()
	if openErr != nil {
		t.Fatalf("open Managed Storage root: %v", openErr)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close Managed Storage root: %v", err)
		}
	})
	albumPath := filepath.Join("library", "album")
	if err := ensureDirectory(root, storage.root, albumPath, 0o750); err != nil {
		t.Fatalf("create canonical Album directory: %v", err)
	}
	targetPath := filepath.Join(albumPath, "cover.png")
	artworks := [][]byte{[]byte("first artwork"), []byte("second artwork")}
	type writeResult struct {
		created bool
		err     error
	}
	results := make(chan writeResult, len(artworks))
	start := make(chan struct{})
	for _, artwork := range artworks {
		artwork := artwork
		go func() {
			<-start
			hash := sha256.Sum256(artwork)
			created, writeErr := writeRootedArtwork(root, storage.root, targetPath, artwork, fmt.Sprintf("%x", hash))
			results <- writeResult{created: created, err: writeErr}
		}()
	}
	close(start)
	createdCount := 0
	conflictCount := 0
	for range artworks {
		result := <-results
		if result.created {
			createdCount++
		}
		var validationError *ValidationError
		if errors.As(result.err, &validationError) && validationError.Code == "album_artwork_conflict" {
			conflictCount++
		}
	}
	if createdCount != 1 || conflictCount != 1 {
		t.Fatalf("concurrent Album Artwork results: created = %d, conflicts = %d", createdCount, conflictCount)
	}
	storedArtwork, err := root.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read winning Album Artwork: %v", err)
	}
	if !bytes.Equal(storedArtwork, artworks[0]) && !bytes.Equal(storedArtwork, artworks[1]) {
		t.Fatalf("winning Album Artwork was overwritten with unexpected bytes: %q", storedArtwork)
	}
}

func newStorageSafetyRouter(t *testing.T, configuration config.Config, capacity storageCapacity) http.Handler {
	t.Helper()
	database := testutil.OpenMigratedDB(t)
	module := newModule(database, configuration, library.NewMediaInspector(), capacity)
	router := chi.NewRouter()
	module.RegisterRoutes(router)
	return router
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

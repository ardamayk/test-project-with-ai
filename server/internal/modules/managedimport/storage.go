package managedimport

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/google/uuid"
)

const MAX_SLUG_BYTES = 80

var slugSeparators = regexp.MustCompile(`[^a-z0-9]+`)

type Storage struct {
	root                string
	initializationError error
	reserveBytes        int64
	fileLimit           int64
	batchLimit          int64
	availableBytes      storageCapacity
}

type StorageLimits struct {
	ReserveBytes int64
	FileBytes    int64
	BatchBytes   int64
}

type StorageRequirement struct {
	SelectedBytes int64
	// TemporaryBytes includes artwork plus replacement or migration copies that must coexist during commit.
	TemporaryBytes int64
}

type storageCapacity func(string) (int64, error)

type stagedUpload struct {
	Path   string
	Size   int64
	SHA256 string
}

type commitIdentity struct {
	AlbumArtistID         string
	AlbumID               string
	TrackID               string
	ExistingArtworkPath   string
	ExistingArtworkSHA256 string
}

type placedFiles struct {
	AudioPath       string
	ArtworkPath     string
	audioRelative   string
	artworkRelative string
	stagedRelative  string
	artworkCreated  bool
}

func NewStorage(root string, limits StorageLimits) *Storage {
	return newStorage(root, limits, availableStorageBytes)
}

func newStorage(root string, limits StorageLimits, capacity storageCapacity) *Storage {
	absoluteRoot, err := filepath.Abs(root)
	if strings.TrimSpace(root) == "" {
		err = errors.New("managed storage root is not configured")
	} else if limits.ReserveBytes < 0 || limits.FileBytes <= 0 || limits.BatchBytes <= 0 {
		err = errors.New("managed storage limits are invalid")
	} else if limits.FileBytes == math.MaxInt64 || limits.BatchBytes == math.MaxInt64 {
		err = errors.New("managed import limits exceed supported byte counts")
	}
	return &Storage{
		root:                absoluteRoot,
		initializationError: err,
		reserveBytes:        limits.ReserveBytes,
		fileLimit:           limits.FileBytes,
		batchLimit:          limits.BatchBytes,
		availableBytes:      capacity,
	}
}

func (storage *Storage) StageUpload(source io.Reader, contentLength int64) (upload stagedUpload, returnErr error) {
	if err := storage.validateUploadLength(contentLength); err != nil {
		return stagedUpload{}, err
	}
	if contentLength > 0 {
		if err := storage.Preflight(StorageRequirement{SelectedBytes: contentLength}); err != nil {
			return stagedUpload{}, err
		}
	}
	root, err := storage.openRoot()
	if err != nil {
		return stagedUpload{}, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	if err := ensureDirectory(root, ".staging", 0o700); err != nil {
		return stagedUpload{}, err
	}
	return storage.writeStagedUpload(root, source)
}

func (storage *Storage) writeStagedUpload(root *os.Root, source io.Reader) (stagedUpload, error) {
	relativePath := filepath.Join(".staging", ".import-"+uuid.NewString()+".upload")
	file, err := root.OpenFile(relativePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return stagedUpload{}, fmt.Errorf("create Managed Import staging file: %w", err)
	}
	hash := sha256.New()
	streamLimit, limitErr := storage.streamLimit()
	destination := io.MultiWriter(file, hash)
	written, copyErr := io.Copy(&capacityWriter{storage: storage, destination: destination}, io.LimitReader(source, streamLimit+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written > streamLimit {
		removeErr := removeRootedFile(root, relativePath, "Managed Import staging file")
		if written > streamLimit {
			return stagedUpload{}, errors.Join(limitErr, removeErr)
		}
		return stagedUpload{}, errors.Join(copyErr, closeErr, removeErr)
	}
	return stagedUpload{
		Path:   storage.absolutePath(relativePath),
		Size:   written,
		SHA256: hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func (storage *Storage) validateUploadLength(contentLength int64) error {
	if contentLength > storage.fileLimit {
		return ErrUploadTooLarge
	}
	if contentLength > storage.batchLimit {
		return ErrBatchTooLarge
	}
	return nil
}

func (storage *Storage) Preflight(requirement StorageRequirement) error {
	if requirement.SelectedBytes < 0 || requirement.TemporaryBytes < 0 {
		return errors.New("managed storage preflight byte counts must not be negative")
	}
	if err := storage.ensureRoot(); err != nil {
		return err
	}
	requiredBytes, err := addByteCounts(
		storage.reserveBytes,
		requirement.SelectedBytes,
		requirement.TemporaryBytes,
	)
	if err != nil {
		return err
	}
	availableBytes, err := storage.availableBytes(storage.root)
	if err != nil {
		return fmt.Errorf("inspect Managed Storage capacity: %w", err)
	}
	if availableBytes < requiredBytes {
		return fmt.Errorf("%w: %d bytes available, %d bytes required", ErrInsufficientStorage, availableBytes, requiredBytes)
	}
	return nil
}

func (storage *Storage) StagedFileSize(path string) (size int64, returnErr error) {
	relativePath, err := storage.relativePath(path)
	if err != nil {
		return 0, err
	}
	root, err := storage.openRoot()
	if err != nil {
		return 0, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	if symlinkErr := rejectSymlinks(root, relativePath); symlinkErr != nil {
		return 0, symlinkErr
	}
	info, err := root.Stat(relativePath)
	if err != nil {
		return 0, fmt.Errorf("stat staged Managed Track: %w", err)
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("%w: staged Managed Track is not a regular file", ErrUnsafeStoragePath)
	}
	return info.Size(), nil
}

func (storage *Storage) RemoveStaged(path string) (returnErr error) {
	relativePath, err := storage.relativePath(path)
	if err != nil {
		return err
	}
	root, err := storage.openRoot()
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	return removeRootedFile(root, relativePath, "Managed Import staging file")
}

func (storage *Storage) Place(stagedPath string, inspection library.MediaInspection, identity commitIdentity) (placement placedFiles, returnErr error) {
	root, err := storage.openRoot()
	if err != nil {
		return placedFiles{}, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	placement, err = storage.planPlacement(stagedPath, inspection, identity)
	if err != nil {
		return placedFiles{}, err
	}
	if err := ensureDirectory(root, filepath.Dir(placement.audioRelative), 0o750); err != nil {
		return placedFiles{}, err
	}
	if err := storage.prepareArtwork(root, &placement, inspection, identity); err != nil {
		return placedFiles{}, err
	}
	if err := root.Rename(placement.stagedRelative, placement.audioRelative); err != nil {
		return placedFiles{}, fmt.Errorf("place Managed Track at Canonical Library Path: %w", err)
	}
	if err := verifyRootedFileHash(root, placement.audioRelative, inspection.FileSHA256); err != nil {
		return placedFiles{}, errors.Join(err, restoreRootedFile(root, placement.audioRelative, placement.stagedRelative))
	}
	if identity.ExistingArtworkPath != "" {
		return placement, nil
	}
	if err := writeRootedArtwork(root, placement.artworkRelative, inspection.AlbumArtwork.Data); err != nil {
		return placedFiles{}, errors.Join(err, restoreRootedFile(root, placement.audioRelative, placement.stagedRelative))
	}
	placement.artworkCreated = true
	return placement, nil
}

func (storage *Storage) planPlacement(stagedPath string, inspection library.MediaInspection, identity commitIdentity) (placedFiles, error) {
	stagedRelative, err := storage.relativePath(stagedPath)
	if err != nil {
		return placedFiles{}, err
	}
	extension, err := sourceExtension(inspection.Audio.Format)
	if err != nil {
		return placedFiles{}, err
	}
	metadata := inspection.Metadata
	albumRelative := filepath.Join(
		"library",
		slug(metadata.AlbumArtists[0])+"-"+identity.AlbumArtistID,
		slug(metadata.Album)+"-"+identity.AlbumID,
	)
	audioFilename := fmt.Sprintf("%02d-%02d-%s-%s%s", metadata.DiscPosition.Number, metadata.TrackPosition.Number, slug(metadata.Title), identity.TrackID, extension)
	audioRelative := filepath.Join(albumRelative, audioFilename)
	artworkRelative := filepath.Join(albumRelative, "cover"+artworkExtension(inspection.AlbumArtwork.MIMEType))
	return placedFiles{
		AudioPath:       storage.absolutePath(audioRelative),
		ArtworkPath:     storage.absolutePath(artworkRelative),
		audioRelative:   audioRelative,
		artworkRelative: artworkRelative,
		stagedRelative:  stagedRelative,
	}, nil
}

func (storage *Storage) prepareArtwork(root *os.Root, placement *placedFiles, inspection library.MediaInspection, identity commitIdentity) error {
	if identity.ExistingArtworkPath != "" {
		if identity.ExistingArtworkSHA256 != inspection.AlbumArtwork.SHA256 {
			return &ValidationError{Code: "album_artwork_conflict", Field: "artwork", Err: errors.New("embedded Album Artwork differs from the existing Album")}
		}
		existingRelative, err := storage.relativePath(identity.ExistingArtworkPath)
		if err != nil {
			return err
		}
		if existingRelative != placement.artworkRelative {
			return fmt.Errorf("%w: existing Album Artwork path is not canonical", ErrUnsafeStoragePath)
		}
		placement.ArtworkPath = identity.ExistingArtworkPath
		placement.artworkRelative = existingRelative
		if err := verifyRootedFileHash(root, existingRelative, identity.ExistingArtworkSHA256); err != nil {
			return fmt.Errorf("verify existing Album Artwork: %w", err)
		}
		return nil
	}
	if _, err := root.Stat(placement.artworkRelative); err == nil {
		return fmt.Errorf("canonical Album Artwork already exists at %q", placement.ArtworkPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect Canonical Album Artwork path: %w", err)
	}
	return nil
}

func (storage *Storage) Rollback(placement placedFiles) (returnErr error) {
	root, err := storage.openRoot()
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	var removeArtworkErr error
	if placement.artworkCreated {
		removeArtworkErr = removeRootedFile(root, placement.artworkRelative, "Canonical Album Artwork")
	}
	moveAudioErr := restoreRootedFile(root, placement.audioRelative, placement.stagedRelative)
	return errors.Join(removeArtworkErr, moveAudioErr)
}

func (storage *Storage) openRoot() (*os.Root, error) {
	if err := storage.ensureRoot(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(storage.root)
	if err != nil {
		return nil, fmt.Errorf("open Managed Storage root: %w", err)
	}
	return root, nil
}

func (storage *Storage) ensureRoot() error {
	if storage.initializationError != nil {
		return storage.initializationError
	}
	if err := os.MkdirAll(storage.root, 0o700); err != nil {
		return fmt.Errorf("create Managed Storage root: %w", err)
	}
	info, err := os.Lstat(storage.root)
	if err != nil {
		return fmt.Errorf("inspect Managed Storage root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: Managed Storage root is a symbolic link", ErrUnsafeStoragePath)
	}
	return nil
}

func (storage *Storage) relativePath(path string) (string, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve Managed Storage path %q: %w", path, err)
	}
	relativePath, err := filepath.Rel(storage.root, absolutePath)
	if err != nil {
		return "", fmt.Errorf("resolve path relative to Managed Storage: %w", err)
	}
	if relativePath == "." || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) || filepath.IsAbs(relativePath) {
		return "", fmt.Errorf("%w: path %q escapes Managed Storage", ErrUnsafeStoragePath, path)
	}
	return relativePath, nil
}

func (storage *Storage) absolutePath(relativePath string) string {
	return filepath.Join(storage.root, relativePath)
}

func ensureDirectory(root *os.Root, path string, mode os.FileMode) error {
	currentPath := ""
	for _, component := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		currentPath = filepath.Join(currentPath, component)
		info, err := root.Lstat(currentPath)
		if errors.Is(err, os.ErrNotExist) {
			if mkdirErr := root.Mkdir(currentPath, mode); mkdirErr != nil {
				return fmt.Errorf("create Managed Storage directory %q: %w", currentPath, mkdirErr)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect Managed Storage directory %q: %w", currentPath, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: directory %q is a symbolic link", ErrUnsafeStoragePath, currentPath)
		}
		if !info.IsDir() {
			return fmt.Errorf("managed storage path %q is not a directory", currentPath)
		}
	}
	if err := root.Chmod(path, mode); err != nil {
		return fmt.Errorf("protect Managed Storage directory %q: %w", path, err)
	}
	return nil
}

func rejectSymlinks(root *os.Root, path string) error {
	currentPath := ""
	for _, component := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		currentPath = filepath.Join(currentPath, component)
		info, err := root.Lstat(currentPath)
		if err != nil {
			return fmt.Errorf("inspect Managed Storage path %q: %w", currentPath, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: path %q is a symbolic link", ErrUnsafeStoragePath, currentPath)
		}
	}
	return nil
}

func verifyRootedFileHash(root *os.Root, path, expectedHash string) error {
	if err := rejectSymlinks(root, path); err != nil {
		return err
	}
	file, err := root.Open(path)
	if err != nil {
		return fmt.Errorf("open Managed Storage file for verification: %w", err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return fmt.Errorf("hash Managed Storage file: %w", errors.Join(copyErr, closeErr))
	}
	actualHash := hex.EncodeToString(hash.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("verify Managed Storage file hash: got %s, want %s", actualHash, expectedHash)
	}
	return nil
}

func writeRootedArtwork(root *os.Root, path string, data []byte) error {
	temporaryPath := filepath.Join(filepath.Dir(path), ".cover-"+uuid.NewString())
	temporary, err := root.OpenFile(temporaryPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create temporary Album Artwork: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return errors.Join(fmt.Errorf("write Album Artwork: %w", err), temporary.Close(), removeRootedFile(root, temporaryPath, "temporary Album Artwork"))
	}
	if err := temporary.Close(); err != nil {
		return errors.Join(fmt.Errorf("close Album Artwork: %w", err), removeRootedFile(root, temporaryPath, "temporary Album Artwork"))
	}
	if err := root.Rename(temporaryPath, path); err != nil {
		return errors.Join(fmt.Errorf("place Album Artwork: %w", err), removeRootedFile(root, temporaryPath, "temporary Album Artwork"))
	}
	return nil
}

func removeRootedFile(root *os.Root, path, description string) error {
	err := root.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("remove %s %q: %w", description, path, err)
}

func restoreRootedFile(root *os.Root, audioPath, stagedPath string) error {
	if err := root.Rename(audioPath, stagedPath); err != nil {
		return fmt.Errorf("restore Managed Import staging file from %q to %q: %w", audioPath, stagedPath, err)
	}
	return nil
}

func addByteCounts(values ...int64) (int64, error) {
	var total int64
	for _, value := range values {
		if value > math.MaxInt64-total {
			return 0, errors.New("managed storage preflight byte count overflow")
		}
		total += value
	}
	return total, nil
}

type capacityWriter struct {
	storage     *Storage
	destination io.Writer
}

func (writer *capacityWriter) Write(buffer []byte) (int, error) {
	if err := writer.storage.Preflight(StorageRequirement{SelectedBytes: int64(len(buffer))}); err != nil {
		return 0, err
	}
	return writer.destination.Write(buffer)
}

func (storage *Storage) streamLimit() (int64, error) {
	if storage.batchLimit < storage.fileLimit {
		return storage.batchLimit, ErrBatchTooLarge
	}
	return storage.fileLimit, ErrUploadTooLarge
}

func sourceExtension(format string) (string, error) {
	switch format {
	case "flac":
		return ".flac", nil
	default:
		return "", &ValidationError{Code: "unsupported_format", Field: "format", Err: fmt.Errorf("validated Source Audio Format %q has no canonical extension", format)}
	}
}

func artworkExtension(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

func slug(value string) string {
	value = strings.ToLower(value)
	value = strings.Map(func(character rune) rune {
		if character > unicode.MaxASCII {
			return '-'
		}
		return character
	}, value)
	value = strings.Trim(slugSeparators.ReplaceAllString(value, "-"), "-")
	if len(value) > MAX_SLUG_BYTES {
		value = strings.Trim(value[:MAX_SLUG_BYTES], "-")
	}
	if value == "" {
		return "untitled"
	}
	return value
}

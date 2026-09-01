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
	} else if filepath.Clean(absoluteRoot) == filepath.VolumeName(absoluteRoot)+string(filepath.Separator) {
		err = fmt.Errorf("%w: filesystem root cannot be Managed Storage", ErrUnsafeStoragePath)
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
	defer func() { returnErr = errors.Join(returnErr, closeManagedStorageRoot(root)) }()
	if err := ensureDirectory(root, storage.root, ".staging", 0o700); err != nil {
		return stagedUpload{}, err
	}
	return storage.writeStagedUpload(root, source)
}

func (storage *Storage) UploadReservationSize(contentLength int64) (int64, error) {
	if err := storage.validateUploadLength(contentLength); err != nil {
		return 0, err
	}
	if contentLength >= 0 {
		return contentLength, nil
	}
	streamLimit, _ := storage.streamLimit()
	return streamLimit, nil
}

func (storage *Storage) writeStagedUpload(root *os.Root, source io.Reader) (stagedUpload, error) {
	relativePath := filepath.Join(".staging", ".import-"+uuid.NewString()+".upload")
	file, err := root.OpenFile(relativePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return stagedUpload{}, fmt.Errorf("create Managed Import staging file: %w", err)
	}
	if err := restrictManagedStoragePath(storage.root, relativePath, false); err != nil {
		return stagedUpload{}, errors.Join(err, closeManagedStorageFile(file, "Managed Import staging file"), removeRootedFile(root, relativePath, "Managed Import staging file"))
	}
	hash := sha256.New()
	streamLimit, limitErr := storage.streamLimit()
	destination := io.MultiWriter(file, hash)
	written, copyErr := io.Copy(&capacityWriter{storage: storage, destination: destination}, io.LimitReader(source, streamLimit+1))
	closeErr := closeManagedStorageFile(file, "Managed Import staging file")
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
	defer func() { returnErr = errors.Join(returnErr, closeManagedStorageRoot(root)) }()
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
	defer func() { returnErr = errors.Join(returnErr, closeManagedStorageRoot(root)) }()
	return removeRootedFile(root, relativePath, "Managed Import staging file")
}

func (storage *Storage) Place(stagedPath string, inspection library.MediaInspection, identity commitIdentity) (placement placedFiles, returnErr error) {
	root, openErr := storage.openRoot()
	if openErr != nil {
		return placedFiles{}, openErr
	}
	defer func() { returnErr = errors.Join(returnErr, closeManagedStorageRoot(root)) }()
	plannedPlacement, planErr := storage.planPlacement(stagedPath, inspection, identity)
	if planErr != nil {
		return placedFiles{}, planErr
	}
	placement = plannedPlacement
	if err := ensureDirectory(root, storage.root, filepath.Dir(placement.audioRelative), 0o750); err != nil {
		return placedFiles{}, err
	}
	shouldCreateArtwork, prepareErr := storage.prepareArtwork(root, &placement, inspection, identity)
	if prepareErr != nil {
		return placedFiles{}, prepareErr
	}
	if err := root.Rename(placement.stagedRelative, placement.audioRelative); err != nil {
		return placedFiles{}, fmt.Errorf("place Managed Track at Canonical Library Path: %w", err)
	}
	if err := verifyRootedFileHash(root, placement.audioRelative, inspection.FileSHA256); err != nil {
		return placedFiles{}, errors.Join(err, restoreRootedFile(root, placement.audioRelative, placement.stagedRelative))
	}
	if !shouldCreateArtwork {
		return placement, nil
	}
	artworkCreated, err := writeRootedArtwork(root, storage.root, placement.artworkRelative, inspection.AlbumArtwork.Data, inspection.AlbumArtwork.SHA256)
	if err != nil {
		return placedFiles{}, errors.Join(err, restoreRootedFile(root, placement.audioRelative, placement.stagedRelative))
	}
	placement.artworkCreated = artworkCreated
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
	artworkRelative := filepath.Join(albumRelative, "cover"+artworkExtension(inspection.AlbumArtwork.MIMEType))
	if identity.ExistingArtworkPath != "" {
		artworkRelative, albumRelative, err = storage.existingCanonicalAlbum(identity, inspection)
		if err != nil {
			return placedFiles{}, err
		}
	}
	audioFilename := fmt.Sprintf("%02d-%02d-%s-%s%s", metadata.DiscPosition.Number, metadata.TrackPosition.Number, slug(metadata.Title), identity.TrackID, extension)
	audioRelative := filepath.Join(albumRelative, audioFilename)
	return placedFiles{
		AudioPath:       storage.absolutePath(audioRelative),
		ArtworkPath:     storage.absolutePath(artworkRelative),
		audioRelative:   audioRelative,
		artworkRelative: artworkRelative,
		stagedRelative:  stagedRelative,
	}, nil
}

func (storage *Storage) existingCanonicalAlbum(identity commitIdentity, inspection library.MediaInspection) (string, string, error) {
	artworkRelative, err := storage.relativePath(identity.ExistingArtworkPath)
	if err != nil {
		return "", "", err
	}
	albumRelative := filepath.Dir(artworkRelative)
	parts := strings.Split(filepath.Clean(albumRelative), string(filepath.Separator))
	isCanonical := len(parts) == 3 && parts[0] == "library" &&
		strings.HasSuffix(parts[1], "-"+identity.AlbumArtistID) &&
		strings.HasSuffix(parts[2], "-"+identity.AlbumID) &&
		filepath.Base(artworkRelative) == "cover"+artworkExtension(inspection.AlbumArtwork.MIMEType)
	if !isCanonical {
		return "", "", fmt.Errorf("%w: existing Album Artwork path is not canonical", ErrUnsafeStoragePath)
	}
	return artworkRelative, albumRelative, nil
}

func (storage *Storage) prepareArtwork(root *os.Root, placement *placedFiles, inspection library.MediaInspection, identity commitIdentity) (bool, error) {
	if identity.ExistingArtworkPath != "" {
		if identity.ExistingArtworkSHA256 != inspection.AlbumArtwork.SHA256 {
			return false, &ValidationError{
				Code:   "album_artwork_conflict",
				Field:  "artwork",
				Reason: "embedded Album Artwork differs from the existing Album",
				Err:    errors.New("embedded Album Artwork differs from the existing Album"),
			}
		}
		placement.ArtworkPath = identity.ExistingArtworkPath
		if err := verifyRootedFileHash(root, placement.artworkRelative, identity.ExistingArtworkSHA256); err != nil {
			return false, fmt.Errorf("verify existing Album Artwork: %w", err)
		}
		return false, nil
	}
	if _, err := root.Stat(placement.artworkRelative); err == nil {
		if verifyErr := verifyMatchingArtwork(root, placement.artworkRelative, inspection.AlbumArtwork.SHA256); verifyErr != nil {
			return false, verifyErr
		}
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect Canonical Album Artwork path: %w", err)
	}
	return true, nil
}

func (storage *Storage) Rollback(placement placedFiles) (returnErr error) {
	root, err := storage.openRoot()
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, closeManagedStorageRoot(root)) }()
	var removeArtworkErr error
	if placement.artworkCreated {
		removeArtworkErr = removeRootedFile(root, placement.artworkRelative, "Canonical Album Artwork")
	}
	return errors.Join(removeArtworkErr, restoreRootedFile(root, placement.audioRelative, placement.stagedRelative))
}

func (storage *Storage) openRoot() (*os.Root, error) {
	if storage.initializationError != nil {
		return nil, storage.initializationError
	}
	root, err := openManagedStorageRoot(storage.root)
	if err != nil {
		return nil, fmt.Errorf("open Managed Storage root: %w", err)
	}
	openedInfo, err := root.Stat(".")
	if err != nil {
		return nil, errors.Join(fmt.Errorf("inspect opened Managed Storage root: %w", err), closeManagedStorageRoot(root))
	}
	pathInfo, err := os.Lstat(storage.root)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("inspect Managed Storage root identity: %w", err), closeManagedStorageRoot(root))
	}
	if !os.SameFile(pathInfo, openedInfo) {
		return nil, errors.Join(fmt.Errorf("%w: Managed Storage root changed while opening", ErrUnsafeStoragePath), closeManagedStorageRoot(root))
	}
	return root, nil
}

func (storage *Storage) ensureRoot() error {
	root, err := storage.openRoot()
	if err != nil {
		return err
	}
	return closeManagedStorageRoot(root)
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

func ensureDirectory(root *os.Root, absoluteRoot, path string, mode os.FileMode) error {
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
	return restrictManagedStoragePath(absoluteRoot, path, true)
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
	closeErr := closeManagedStorageFile(file, "Managed Storage verification file")
	if copyErr != nil || closeErr != nil {
		return fmt.Errorf("hash Managed Storage file: %w", errors.Join(copyErr, closeErr))
	}
	actualHash := hex.EncodeToString(hash.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("verify Managed Storage file hash: got %s, want %s", actualHash, expectedHash)
	}
	return nil
}

func writeRootedArtwork(root *os.Root, absoluteRoot, path string, data []byte, expectedHash string) (bool, error) {
	temporaryPath := filepath.Join(filepath.Dir(path), ".cover-"+uuid.NewString())
	temporary, err := root.OpenFile(temporaryPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return false, fmt.Errorf("create temporary Album Artwork: %w", err)
	}
	if err := restrictManagedStoragePath(absoluteRoot, temporaryPath, false); err != nil {
		return false, errors.Join(err, closeManagedStorageFile(temporary, "temporary Album Artwork"), removeRootedFile(root, temporaryPath, "temporary Album Artwork"))
	}
	if _, err := temporary.Write(data); err != nil {
		return false, errors.Join(fmt.Errorf("write Album Artwork: %w", err), closeManagedStorageFile(temporary, "temporary Album Artwork"), removeRootedFile(root, temporaryPath, "temporary Album Artwork"))
	}
	if err := closeManagedStorageFile(temporary, "temporary Album Artwork"); err != nil {
		return false, errors.Join(err, removeRootedFile(root, temporaryPath, "temporary Album Artwork"))
	}
	if err := root.Link(temporaryPath, path); errors.Is(err, os.ErrExist) {
		return false, errors.Join(verifyMatchingArtwork(root, path, expectedHash), removeRootedFile(root, temporaryPath, "temporary Album Artwork"))
	} else if err != nil {
		return false, errors.Join(fmt.Errorf("place Album Artwork: %w", err), removeRootedFile(root, temporaryPath, "temporary Album Artwork"))
	}
	if err := removeRootedFile(root, temporaryPath, "temporary Album Artwork"); err != nil {
		return false, errors.Join(err, removeRootedFile(root, path, "Canonical Album Artwork"))
	}
	return true, nil
}

func verifyMatchingArtwork(root *os.Root, path, expectedHash string) error {
	if err := verifyRootedFileHash(root, path, expectedHash); err != nil {
		return &ValidationError{
			Code:   "album_artwork_conflict",
			Field:  "artwork",
			Reason: "canonical Album Artwork differs from the selected Album",
			Err:    err,
		}
	}
	return nil
}

func closeManagedStorageRoot(root *os.Root) error {
	name := root.Name()
	if err := root.Close(); err != nil {
		return fmt.Errorf("close Managed Storage root %q: %w", name, err)
	}
	return nil
}

func closeManagedStorageFile(file *os.File, description string) error {
	name := file.Name()
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s %q: %w", description, name, err)
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
	case "m4a":
		return ".m4a", nil
	case "mp3":
		return ".mp3", nil
	case "ogg":
		return ".ogg", nil
	case "opus":
		return ".opus", nil
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

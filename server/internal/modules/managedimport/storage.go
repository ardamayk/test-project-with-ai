package managedimport

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
)

var slugSeparators = regexp.MustCompile(`[^a-z0-9]+`)

type Storage struct {
	root string
}

type commitIdentity struct {
	AlbumID               string
	TrackID               string
	ExistingArtworkPath   string
	ExistingArtworkSHA256 string
}

type placedFiles struct {
	AudioPath      string
	ArtworkPath    string
	stagedPath     string
	artworkCreated bool
}

func NewStorage(root string) *Storage {
	return &Storage{root: root}
}

func (storage *Storage) StageUpload(jobID string, source io.Reader, contentLength int64) (string, int64, error) {
	if contentLength > MAX_UPLOAD_SIZE_BYTES {
		return "", 0, ErrUploadTooLarge
	}
	stagingPath := filepath.Join(storage.root, ".staging")
	if err := os.MkdirAll(stagingPath, 0o700); err != nil {
		return "", 0, fmt.Errorf("create Managed Import staging directory: %w", err)
	}
	filePath := filepath.Join(stagingPath, jobID+".upload")
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, fmt.Errorf("create Managed Import staging file: %w", err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(source, MAX_UPLOAD_SIZE_BYTES+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written > MAX_UPLOAD_SIZE_BYTES {
		removeErr := removeStagedFile(filePath)
		if written > MAX_UPLOAD_SIZE_BYTES {
			return "", 0, errors.Join(ErrUploadTooLarge, removeErr)
		}
		return "", 0, errors.Join(copyErr, closeErr, removeErr)
	}
	return filePath, written, nil
}

func (storage *Storage) Place(stagedPath string, inspection library.MediaInspection, identity commitIdentity) (placedFiles, error) {
	metadata := inspection.Metadata
	albumDirectory := filepath.Join(
		storage.root,
		"library",
		slug(metadata.AlbumArtists[0]),
		slug(metadata.Album)+"-"+identity.AlbumID,
	)
	if identity.ExistingArtworkPath != "" {
		albumDirectory = filepath.Dir(identity.ExistingArtworkPath)
	}
	if err := os.MkdirAll(albumDirectory, 0o755); err != nil {
		return placedFiles{}, fmt.Errorf("create Canonical Album directory: %w", err)
	}
	audioFilename := fmt.Sprintf("%02d-%02d-%s-%s.flac", metadata.DiscPosition.Number, metadata.TrackPosition.Number, slug(metadata.Title), identity.TrackID)
	placement := placedFiles{
		AudioPath:   filepath.Join(albumDirectory, audioFilename),
		ArtworkPath: filepath.Join(albumDirectory, "cover"+artworkExtension(inspection.AlbumArtwork.MIMEType)),
		stagedPath:  stagedPath,
	}
	if identity.ExistingArtworkPath != "" {
		if identity.ExistingArtworkSHA256 != inspection.AlbumArtwork.SHA256 {
			return placedFiles{}, &ValidationError{
				Code:   "album_artwork_conflict",
				Field:  "artwork",
				Reason: "embedded Album Artwork differs from the existing Album",
				Err:    errors.New("embedded Album Artwork differs from the existing Album"),
			}
		}
		placement.ArtworkPath = identity.ExistingArtworkPath
		if err := verifyFileHash(placement.ArtworkPath, identity.ExistingArtworkSHA256); err != nil {
			return placedFiles{}, fmt.Errorf("verify existing Album Artwork: %w", err)
		}
	} else if _, err := os.Stat(placement.ArtworkPath); err == nil {
		return placedFiles{}, fmt.Errorf("canonical Album Artwork already exists at %q", placement.ArtworkPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return placedFiles{}, fmt.Errorf("inspect Canonical Album Artwork path: %w", err)
	}
	if err := os.Rename(stagedPath, placement.AudioPath); err != nil {
		return placedFiles{}, fmt.Errorf("place Managed Track at Canonical Library Path: %w", err)
	}
	if err := verifyFileHash(placement.AudioPath, inspection.FileSHA256); err != nil {
		return placedFiles{}, errors.Join(err, restoreStagedFile(placement.AudioPath, stagedPath))
	}
	if identity.ExistingArtworkPath != "" {
		return placement, nil
	}
	if err := writeArtwork(placement.ArtworkPath, inspection.AlbumArtwork.Data); err != nil {
		return placedFiles{}, errors.Join(err, restoreStagedFile(placement.AudioPath, stagedPath))
	}
	placement.artworkCreated = true
	return placement, nil
}

func (storage *Storage) Rollback(placement placedFiles) error {
	var removeArtworkErr error
	if placement.artworkCreated {
		removeArtworkErr = os.Remove(placement.ArtworkPath)
		if errors.Is(removeArtworkErr, os.ErrNotExist) {
			removeArtworkErr = nil
		}
	}
	moveAudioErr := os.Rename(placement.AudioPath, placement.stagedPath)
	return errors.Join(removeArtworkErr, moveAudioErr)
}

func verifyFileHash(path string, expectedHash string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open canonical Managed Track for verification: %w", err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return fmt.Errorf("hash canonical Managed Track: %w", errors.Join(copyErr, closeErr))
	}
	actualHash := hex.EncodeToString(hash.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("verify canonical Managed Track hash: got %s, want %s", actualHash, expectedHash)
	}
	return nil
}

func writeArtwork(path string, data []byte) (err error) {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".cover-*")
	if err != nil {
		return fmt.Errorf("create temporary Album Artwork: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err != nil {
			removeErr := os.Remove(temporaryPath)
			if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("remove temporary Album Artwork %q: %w", temporaryPath, removeErr))
			}
		}
	}()
	if chmodErr := temporary.Chmod(0o600); chmodErr != nil {
		return errors.Join(fmt.Errorf("protect temporary Album Artwork: %w", chmodErr), closeFile(temporary, "temporary Album Artwork"))
	}
	if _, writeErr := temporary.Write(data); writeErr != nil {
		return errors.Join(fmt.Errorf("write Album Artwork: %w", writeErr), closeFile(temporary, "temporary Album Artwork"))
	}
	if closeErr := temporary.Close(); closeErr != nil {
		return fmt.Errorf("close Album Artwork: %w", closeErr)
	}
	if renameErr := os.Rename(temporaryPath, path); renameErr != nil {
		return fmt.Errorf("place Album Artwork: %w", renameErr)
	}
	return nil
}

func removeStagedFile(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("remove Managed Import staging file %q: %w", path, err)
}

func restoreStagedFile(audioPath, stagedPath string) error {
	if err := os.Rename(audioPath, stagedPath); err != nil {
		return fmt.Errorf("restore Managed Import staging file from %q to %q: %w", audioPath, stagedPath, err)
	}
	return nil
}

func closeFile(file *os.File, description string) error {
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", description, err)
	}
	return nil
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
	if value == "" {
		return "untitled"
	}
	return value
}

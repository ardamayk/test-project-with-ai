package managedimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
)

// replacementPlacement describes every Managed Storage path a Track Replacement touches. The replacement
// audio lands on a pending path first so the previous managed file stays untouched until verification passes.
type replacementPlacement struct {
	AudioPath               string
	ArtworkPath             string
	ArtworkMode             string
	stagedRelative          string
	pendingAudioRelative    string
	audioRelative           string
	previousAudioRelative   string
	retiredAudioRelative    string
	pendingArtworkRelative  string
	artworkRelative         string
	previousArtworkRelative string
	retiredArtworkRelative  string
	artworkCreated          bool
}

func (storage *Storage) planReplacementPlacement(stagedPath string, inspection library.MediaInspection, identity commitIdentity, target replacementTarget) (replacementPlacement, error) {
	albumRelative, err := storage.replacementAlbumDirectory(inspection, identity)
	if err != nil {
		return replacementPlacement{}, err
	}
	extension, err := sourceExtension(inspection.Audio.Format)
	if err != nil {
		return replacementPlacement{}, err
	}
	stagedRelative, err := storage.relativePath(stagedPath)
	if err != nil {
		return replacementPlacement{}, err
	}
	planned, err := storage.planPlacementInAlbum(stagedPath, inspection, identity.TrackID, albumRelative, filepath.Join(albumRelative, "cover"+artworkExtension(inspection.AlbumArtwork.MIMEType)))
	if err != nil {
		return replacementPlacement{}, err
	}
	placement := replacementPlacement{
		AudioPath:             planned.AudioPath,
		stagedRelative:        stagedRelative,
		pendingAudioRelative:  filepath.Join(albumRelative, ".replacement-"+identity.TrackID+extension),
		audioRelative:         planned.audioRelative,
		previousAudioRelative: target.RelativePath,
		retiredAudioRelative:  filepath.Join(filepath.Dir(target.RelativePath), ".retired-"+identity.TrackID+filepath.Ext(target.RelativePath)),
	}
	if err := storage.planReplacementArtwork(&placement, planned, inspection, identity, target, albumRelative); err != nil {
		return replacementPlacement{}, err
	}
	return placement, nil
}

func (storage *Storage) replacementAlbumDirectory(inspection library.MediaInspection, identity commitIdentity) (string, error) {
	metadata := inspection.Metadata
	if identity.ExistingArtworkPath == "" {
		return filepath.Join("library", slug(metadata.AlbumArtists[0])+"-"+identity.AlbumArtistID, slug(metadata.Album)+"-"+identity.AlbumID), nil
	}
	artworkRelative, err := storage.relativePath(identity.ExistingArtworkPath)
	if err != nil {
		return "", err
	}
	albumRelative := filepath.Dir(artworkRelative)
	parts := strings.Split(filepath.Clean(albumRelative), string(filepath.Separator))
	if len(parts) != 3 || parts[0] != "library" ||
		!strings.HasSuffix(parts[1], "-"+identity.AlbumArtistID) ||
		!strings.HasSuffix(parts[2], "-"+identity.AlbumID) ||
		!strings.HasPrefix(filepath.Base(artworkRelative), "cover.") {
		return "", fmt.Errorf("%w: existing Album Artwork path is not canonical", ErrUnsafeStoragePath)
	}
	return albumRelative, nil
}

func (storage *Storage) planReplacementArtwork(placement *replacementPlacement, planned placedFiles, inspection library.MediaInspection, identity commitIdentity, target replacementTarget, albumRelative string) error {
	placement.ArtworkPath = planned.ArtworkPath
	placement.artworkRelative = planned.artworkRelative
	isMovingAlbum := identity.AlbumID != target.AlbumID
	if isMovingAlbum && target.IsSoleTrack && target.ArtworkPath != "" {
		previousArtworkRelative, err := storage.relativePath(target.ArtworkPath)
		if err != nil {
			return err
		}
		placement.previousArtworkRelative = previousArtworkRelative
	}
	if identity.ExistingArtworkPath == "" {
		placement.ArtworkMode = REPLACEMENT_ARTWORK_MODE_CREATE
		return nil
	}
	existingRelative, err := storage.relativePath(identity.ExistingArtworkPath)
	if err != nil {
		return err
	}
	if identity.ExistingArtworkSHA256 == inspection.AlbumArtwork.SHA256 {
		placement.ArtworkMode = REPLACEMENT_ARTWORK_MODE_EXISTING
		placement.ArtworkPath = identity.ExistingArtworkPath
		placement.artworkRelative = existingRelative
		return nil
	}
	if isMovingAlbum || !target.IsSoleTrack {
		return albumArtworkConflictError("embedded Album Artwork differs from the existing Album")
	}
	placement.ArtworkMode = REPLACEMENT_ARTWORK_MODE_REPLACE
	placement.pendingArtworkRelative = filepath.Join(albumRelative, ".replacement-cover"+artworkExtension(inspection.AlbumArtwork.MIMEType))
	placement.previousArtworkRelative = existingRelative
	placement.retiredArtworkRelative = filepath.Join(albumRelative, ".retired-cover"+filepath.Ext(existingRelative))
	return nil
}

func albumArtworkConflictError(reason string) error {
	return &ValidationError{Code: ERROR_CODE_ALBUM_ARTWORK_CONFLICT, Field: "artwork", Reason: reason, Err: errors.New(reason)}
}

func (storage *Storage) replacementPlacementFromJournal(journal replacementJournal) (replacementPlacement, error) {
	placement := replacementPlacement{
		AudioPath:      journal.AudioFilePath,
		ArtworkPath:    journal.ArtworkFilePath,
		ArtworkMode:    journal.ArtworkMode,
		artworkCreated: journal.IsArtworkCreated,
	}
	relatives := []struct {
		destination *string
		path        string
	}{
		{&placement.stagedRelative, journal.StagedFilePath},
		{&placement.pendingAudioRelative, journal.PendingAudioPath},
		{&placement.audioRelative, journal.AudioFilePath},
		{&placement.previousAudioRelative, journal.PreviousAudioPath},
		{&placement.retiredAudioRelative, journal.RetiredAudioPath},
		{&placement.artworkRelative, journal.ArtworkFilePath},
		{&placement.pendingArtworkRelative, journal.PendingArtworkPath},
		{&placement.previousArtworkRelative, journal.PreviousArtworkPath},
		{&placement.retiredArtworkRelative, journal.RetiredArtworkPath},
	}
	for _, relative := range relatives {
		if relative.path == "" {
			continue
		}
		resolved, err := storage.relativePath(relative.path)
		if err != nil {
			return replacementPlacement{}, err
		}
		*relative.destination = resolved
	}
	return placement, nil
}

func (storage *Storage) replacementJournalPaths(placement replacementPlacement) (pendingAudio, previousAudio, retiredAudio, pendingArtwork, previousArtwork, retiredArtwork string) {
	optional := func(relative string) string {
		if relative == "" {
			return ""
		}
		return storage.absolutePath(relative)
	}
	return storage.absolutePath(placement.pendingAudioRelative), storage.absolutePath(placement.previousAudioRelative),
		storage.absolutePath(placement.retiredAudioRelative), optional(placement.pendingArtworkRelative),
		optional(placement.previousArtworkRelative), optional(placement.retiredArtworkRelative)
}

// PlaceReplacement moves the staged upload onto its pending canonical path and prepares Album Artwork
// without touching the previous managed file.
func (storage *Storage) PlaceReplacement(placement replacementPlacement, inspection library.MediaInspection, identity commitIdentity, artworkCreated func() error) (_ replacementPlacement, returnErr error) {
	root, err := storage.openRoot()
	if err != nil {
		return replacementPlacement{}, err
	}
	defer func() { returnErr = errors.Join(returnErr, closeManagedStorageRoot(root)) }()
	if err := ensureDirectory(root, storage.root, filepath.Dir(placement.audioRelative), 0o750); err != nil {
		return replacementPlacement{}, err
	}
	if err := root.Rename(placement.stagedRelative, placement.pendingAudioRelative); err != nil {
		return replacementPlacement{}, fmt.Errorf("place replacement Managed Track on pending path: %w", err)
	}
	created, artworkErr := storage.placeReplacementArtwork(root, placement, inspection, identity, artworkCreated)
	if artworkErr != nil {
		return replacementPlacement{}, errors.Join(artworkErr, restoreRootedFile(root, placement.pendingAudioRelative, placement.stagedRelative))
	}
	placement.artworkCreated = created
	return placement, nil
}

func (storage *Storage) placeReplacementArtwork(root *os.Root, placement replacementPlacement, inspection library.MediaInspection, identity commitIdentity, artworkCreated func() error) (bool, error) {
	switch placement.ArtworkMode {
	case REPLACEMENT_ARTWORK_MODE_EXISTING:
		if err := verifyRootedFileHash(root, placement.artworkRelative, identity.ExistingArtworkSHA256); err != nil {
			return false, fmt.Errorf("verify existing Album Artwork: %w", err)
		}
		return false, nil
	case REPLACEMENT_ARTWORK_MODE_REPLACE:
		return writeRootedArtwork(root, storage.root, placement.pendingArtworkRelative, inspection.AlbumArtwork.Data, inspection.AlbumArtwork.SHA256, artworkCreated)
	default:
		return writeRootedArtwork(root, storage.root, placement.artworkRelative, inspection.AlbumArtwork.Data, inspection.AlbumArtwork.SHA256, artworkCreated)
	}
}

// VerifyReplacement hashes the pending audio and artwork copies before anything visible changes.
func (storage *Storage) VerifyReplacement(placement replacementPlacement, audioSHA256, artworkSHA256 string) (returnErr error) {
	root, err := storage.openRoot()
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, closeManagedStorageRoot(root)) }()
	if err := verifyRootedFileHash(root, placement.pendingAudioRelative, audioSHA256); err != nil {
		return fmt.Errorf("verify pending replacement Managed Track: %w", err)
	}
	artworkRelative := placement.artworkRelative
	if placement.ArtworkMode == REPLACEMENT_ARTWORK_MODE_REPLACE {
		artworkRelative = placement.pendingArtworkRelative
	}
	if err := verifyRootedFileHash(root, artworkRelative, artworkSHA256); err != nil {
		return fmt.Errorf("verify replacement Album Artwork: %w", err)
	}
	return nil
}

// SwapReplacement retires the previous managed file and moves the verified replacement onto its canonical path.
func (storage *Storage) SwapReplacement(placement replacementPlacement) (returnErr error) {
	root, err := storage.openRoot()
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, closeManagedStorageRoot(root)) }()
	if err := root.Rename(placement.previousAudioRelative, placement.retiredAudioRelative); err != nil {
		return fmt.Errorf("retire previous Managed Track: %w", err)
	}
	if err := root.Rename(placement.pendingAudioRelative, placement.audioRelative); err != nil {
		return errors.Join(fmt.Errorf("place replacement Managed Track at Canonical Library Path: %w", err),
			restoreRootedFile(root, placement.retiredAudioRelative, placement.previousAudioRelative))
	}
	if placement.ArtworkMode != REPLACEMENT_ARTWORK_MODE_REPLACE {
		return nil
	}
	if err := root.Rename(placement.previousArtworkRelative, placement.retiredArtworkRelative); err != nil {
		return errors.Join(fmt.Errorf("retire previous Album Artwork: %w", err), storage.undoAudioSwap(root, placement))
	}
	if err := root.Rename(placement.pendingArtworkRelative, placement.artworkRelative); err != nil {
		return errors.Join(fmt.Errorf("place replacement Album Artwork: %w", err),
			restoreRootedFile(root, placement.retiredArtworkRelative, placement.previousArtworkRelative), storage.undoAudioSwap(root, placement))
	}
	return nil
}

func (storage *Storage) undoAudioSwap(root *os.Root, placement replacementPlacement) error {
	return errors.Join(
		restoreRootedFile(root, placement.audioRelative, placement.pendingAudioRelative),
		restoreRootedFile(root, placement.retiredAudioRelative, placement.previousAudioRelative),
	)
}

// StreamSwappedReplacement reads the canonical replacement file back to EOF, proving the bytes the Track
// will now serve are readable before the previous file is deleted.
func (storage *Storage) StreamSwappedReplacement(placement replacementPlacement, audioSHA256 string) (returnErr error) {
	root, err := storage.openRoot()
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, closeManagedStorageRoot(root)) }()
	if verifyErr := verifyRootedFileHash(root, placement.audioRelative, audioSHA256); verifyErr != nil {
		return fmt.Errorf("verify canonical replacement Managed Track: %w", verifyErr)
	}
	stream, err := root.Open(placement.audioRelative)
	if err != nil {
		return fmt.Errorf("open canonical replacement stream: %w", err)
	}
	_, streamErr := io.Copy(io.Discard, stream)
	closeErr := closeManagedStorageFile(stream, "canonical replacement stream")
	if streamErr != nil || closeErr != nil {
		return fmt.Errorf("stream canonical replacement Managed Track: %w", errors.Join(streamErr, closeErr))
	}
	return nil
}

// RollbackReplacement restores the previous managed file and returns the replacement to staging so the Import
// Job stays retryable. It inspects the filesystem instead of trusting the journaled phase, so a crash in the
// middle of the swap is undone as well.
func (storage *Storage) RollbackReplacement(placement replacementPlacement) (returnErr error) {
	root, err := storage.openRoot()
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, closeManagedStorageRoot(root)) }()
	swapErr := storage.unswapReplacement(root, placement)
	pendingErr := restoreIfPresent(root, placement.pendingAudioRelative, placement.stagedRelative)
	var artworkErr error
	if placement.artworkCreated {
		createdRelative := placement.artworkRelative
		if placement.ArtworkMode == REPLACEMENT_ARTWORK_MODE_REPLACE {
			createdRelative = placement.pendingArtworkRelative
		}
		artworkErr = removeRootedFile(root, createdRelative, "uncommitted replacement Album Artwork")
	}
	directoryErr := removeEmptyCanonicalDirectories(root, filepath.Dir(placement.audioRelative))
	return errors.Join(swapErr, pendingErr, artworkErr, directoryErr)
}

// unswapReplacement only moves canonical files when the retired copy proves the swap started; before the
// swap the canonical path may still hold the previous file itself.
func (storage *Storage) unswapReplacement(root *os.Root, placement replacementPlacement) error {
	var artworkErr error
	if placement.ArtworkMode == REPLACEMENT_ARTWORK_MODE_REPLACE {
		artworkErr = undoRetiredSwap(root, placement.retiredArtworkRelative, placement.previousArtworkRelative, placement.artworkRelative, placement.pendingArtworkRelative)
	}
	audioErr := undoRetiredSwap(root, placement.retiredAudioRelative, placement.previousAudioRelative, placement.audioRelative, placement.pendingAudioRelative)
	return errors.Join(artworkErr, audioErr)
}

func undoRetiredSwap(root *os.Root, retired, previous, canonical, pending string) error {
	if _, err := root.Lstat(retired); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect retired Managed Storage file %q: %w", retired, err)
	}
	return errors.Join(restoreIfPresent(root, canonical, pending), restoreIfPresent(root, retired, previous))
}

func restoreIfPresent(root *os.Root, from, to string) error {
	if _, err := root.Lstat(from); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect %q before restore: %w", from, err)
	}
	if _, err := root.Lstat(to); err == nil {
		return fmt.Errorf("%w: restore target %q already exists", ErrUnsafeStoragePath, to)
	}
	return restoreRootedFile(root, from, to)
}

// CompleteReplacementFiles deletes the retired previous managed file and any Album Artwork that lost its
// last reference. It runs only after the database commit made the replacement authoritative.
func (storage *Storage) CompleteReplacementFiles(ctx context.Context, journal replacementJournal, placement replacementPlacement) (deletedFiles int, returnErr error) {
	audioRemoved, audioErr := storage.RemoveManagedFile(ctx, journal.RetiredAudioPath, journal.PreviousAudioSHA256)
	if !audioRemoved {
		return 0, audioErr
	}
	deletedFiles = 1
	artworkPath := journal.RetiredArtworkPath
	if artworkPath == "" {
		artworkPath = journal.PreviousArtworkPath
	}
	if artworkPath != "" {
		artworkRemoved, artworkErr := storage.RemoveManagedFile(ctx, artworkPath, journal.PreviousArtworkSHA256)
		if !artworkRemoved {
			return deletedFiles, errors.Join(audioErr, artworkErr)
		}
		deletedFiles++
		audioErr = errors.Join(audioErr, artworkErr)
	}
	root, err := storage.openRoot()
	if err != nil {
		return deletedFiles, errors.Join(audioErr, err)
	}
	defer func() { returnErr = errors.Join(returnErr, closeManagedStorageRoot(root)) }()
	return deletedFiles, errors.Join(audioErr, removeEmptyCanonicalDirectories(root, filepath.Dir(placement.previousAudioRelative)))
}

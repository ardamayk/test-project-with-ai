package managedimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/google/uuid"
)

// replacementTarget is the current state of the Managed Track a Track Replacement will overwrite.
type replacementTarget struct {
	TrackID        string
	Title          string
	Artists        []string
	AlbumArtists   []string
	Album          string
	Year           int
	Genres         []string
	TrackNo        int
	TrackTotal     int
	DiscNo         int
	DiscTotal      int
	DurationMs     int
	Format         string
	Container      string
	Codec          string
	SampleRateHz   int
	ChannelCount   int
	BitDepth       int
	BitrateKbps    int
	SizeBytes      int64
	AlbumID        string
	AlbumKey       string
	FilePath       string
	RelativePath   string
	ContentSHA256  string
	TrackRevision  int
	SourceRevision int
	ArtworkPath    string
	ArtworkSHA256  string
	ArtworkType    string
	IsSoleTrack    bool
	Playlists      []TrackDeletionPlaylistReference
	Queues         []TrackDeletionQueueReference
}

type replacementCommitData struct {
	Job        importJob
	Target     replacementTarget
	Identity   commitIdentity
	Placement  replacementPlacement
	Inspection library.MediaInspection
	AlbumKey   string
}

func (store *Store) CreateReplacementJob(ctx context.Context, trackID string) (Job, error) {
	job := Job{ID: uuid.NewString(), Status: STATUS_UPLOADING, Revision: 1, ReplacesTrackID: trackID}
	_, err := store.database.ExecContext(ctx, `
		INSERT INTO managed_import_jobs (id, status, revision, replace_track_id)
		VALUES (?, ?, ?, ?)`, job.ID, job.Status, job.Revision, trackID)
	if err != nil {
		return Job{}, fmt.Errorf("create Track Replacement job: %w", err)
	}
	return job, nil
}

func loadReplacementTarget(ctx context.Context, queryer deletionQueryer, storage *Storage, trackID string) (replacementTarget, error) {
	target, err := readReplacementTargetRow(ctx, queryer, trackID)
	if err != nil {
		return replacementTarget{}, err
	}
	relativePath, actualSize, err := storage.ResolveManagedFile(target.FilePath, target.ContentSHA256)
	if err != nil {
		return replacementTarget{}, err
	}
	if target.SizeBytes != actualSize {
		return replacementTarget{}, fmt.Errorf("%w: managed file size changed", ErrReplacementConflict)
	}
	target.RelativePath = relativePath
	if target.Artists, err = queryOrderedNames(ctx, queryer, `
		SELECT artists.name FROM track_artists JOIN artists ON artists.id = track_artists.artist_id
		WHERE track_artists.track_id = ? ORDER BY track_artists.position`, trackID); err != nil {
		return replacementTarget{}, err
	}
	if target.AlbumArtists, err = queryOrderedNames(ctx, queryer, `
		SELECT artists.name FROM album_artists JOIN artists ON artists.id = album_artists.artist_id
		WHERE album_artists.album_id = ? ORDER BY album_artists.position`, target.AlbumID); err != nil {
		return replacementTarget{}, err
	}
	if target.Genres, err = queryOrderedNames(ctx, queryer, `
		SELECT genres.name FROM track_genres JOIN genres ON genres.id = track_genres.genre_id
		WHERE track_genres.track_id = ? ORDER BY track_genres.position`, trackID); err != nil {
		return replacementTarget{}, err
	}
	if target.Playlists, err = listDeletionPlaylists(ctx, queryer, trackID); err != nil {
		return replacementTarget{}, err
	}
	if target.Queues, err = listDeletionQueues(ctx, queryer, trackID); err != nil {
		return replacementTarget{}, err
	}
	return target, nil
}

func readReplacementTargetRow(ctx context.Context, queryer deletionQueryer, trackID string) (replacementTarget, error) {
	var target replacementTarget
	var sourceKind string
	var albumKey, artworkPath, artworkSHA256, artworkType, container, codec sql.NullString
	var year, trackNo, trackTotal, discNo, discTotal, sampleRate, bitDepth, channels, bitrateBps sql.NullInt64
	err := queryer.QueryRowContext(ctx, `
		SELECT tracks.title, tracks.album_id, tracks.revision, tracks.track_no, tracks.track_total, tracks.disc_no,
			tracks.disc_total, tracks.duration_ms, tracks.sample_rate_hz, tracks.bit_depth, tracks.channel_count,
			tracks.bitrate_bps, tracks.codec, tracks.container, albums.title, albums.year, albums.identity_key,
			track_sources.source_kind, track_sources.file_path, track_sources.content_sha256, track_sources.size_bytes,
			track_sources.source_format, track_sources.revision,
			album_artwork.file_path, album_artwork.content_sha256, album_artwork.media_type,
			NOT EXISTS (SELECT 1 FROM tracks siblings WHERE siblings.album_id = tracks.album_id AND siblings.id != tracks.id AND siblings.missing_at IS NULL)
		FROM tracks
		JOIN albums ON albums.id = tracks.album_id
		JOIN track_sources ON track_sources.track_id = tracks.id
		LEFT JOIN album_artwork ON album_artwork.album_id = tracks.album_id
		WHERE tracks.id = ? AND tracks.missing_at IS NULL AND tracks.is_pending_commit = 0`, trackID).Scan(
		&target.Title, &target.AlbumID, &target.TrackRevision, &trackNo, &trackTotal, &discNo, &discTotal,
		&target.DurationMs, &sampleRate, &bitDepth, &channels, &bitrateBps, &codec, &container,
		&target.Album, &year, &albumKey, &sourceKind, &target.FilePath, &target.ContentSHA256, &target.SizeBytes,
		&target.Format, &target.SourceRevision, &artworkPath, &artworkSHA256, &artworkType, &target.IsSoleTrack,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return replacementTarget{}, ErrTrackNotFound
	}
	if err != nil {
		return replacementTarget{}, fmt.Errorf("load Track Replacement target: %w", err)
	}
	if sourceKind != "managed" {
		return replacementTarget{}, ErrNotManagedTrack
	}
	target.TrackID = trackID
	target.TrackNo, target.TrackTotal = int(trackNo.Int64), int(trackTotal.Int64)
	target.DiscNo, target.DiscTotal = int(discNo.Int64), int(discTotal.Int64)
	if target.DiscNo == 0 {
		target.DiscNo = 1
	}
	target.SampleRateHz, target.BitDepth, target.ChannelCount = int(sampleRate.Int64), int(bitDepth.Int64), int(channels.Int64)
	target.BitrateKbps = int(bitrateBps.Int64) / BITS_PER_KILOBIT
	target.Codec, target.Container, target.Year = codec.String, container.String, int(year.Int64)
	target.AlbumKey = albumKey.String
	target.ArtworkPath, target.ArtworkSHA256, target.ArtworkType = artworkPath.String, artworkSHA256.String, artworkType.String
	return target, nil
}

func queryOrderedNames(ctx context.Context, queryer deletionQueryer, query string, args ...any) (names []string, returnErr error) {
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list Track Replacement names: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, closeDeletionRows(rows, "Track Replacement names")) }()
	names = []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("read Track Replacement name: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// ResolveReplacementIdentity resolves the Album and Album Artist a replacement file belongs to while keeping the Track ID.
func (store *Store) ResolveReplacementIdentity(ctx context.Context, metadata library.NormalizedMediaMetadata, albumKey, trackID string) (commitIdentity, error) {
	identity, err := store.ResolveCommitIdentity(ctx, metadata, albumKey)
	if err != nil {
		return commitIdentity{}, err
	}
	identity.TrackID = trackID
	return identity, nil
}

func (store *Store) AlbumExists(ctx context.Context, albumKey string) (bool, error) {
	var exists bool
	if err := store.database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM albums WHERE identity_key = ?)`, albumKey).Scan(&exists); err != nil {
		return false, fmt.Errorf("inspect replacement Album: %w", err)
	}
	return exists, nil
}

func (store *Store) MissingArtistNames(ctx context.Context, names []string) ([]string, error) {
	return store.missingNames(ctx, `SELECT EXISTS (SELECT 1 FROM artists WHERE name_normalized = ?)`, names)
}

func (store *Store) MissingGenreNames(ctx context.Context, names []string) ([]string, error) {
	return store.missingNames(ctx, `SELECT EXISTS (SELECT 1 FROM genres WHERE name_normalized = ?)`, names)
}

func (store *Store) missingNames(ctx context.Context, query string, names []string) ([]string, error) {
	missing := []string{}
	seen := map[string]bool{}
	for _, name := range names {
		normalized := normalizeIdentity(name)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		var exists bool
		if err := store.database.QueryRowContext(ctx, query, normalized).Scan(&exists); err != nil {
			return nil, fmt.Errorf("inspect replacement name %q: %w", name, err)
		}
		if !exists {
			missing = append(missing, name)
		}
	}
	return missing, nil
}

// OrphanedArtistNames lists Artists that lose every reference once the target Track adopts the replacement credits.
func (store *Store) OrphanedArtistNames(ctx context.Context, target replacementTarget, metadata library.NormalizedMediaMetadata, removesAlbum bool) (names []string, returnErr error) {
	retained := map[string]bool{}
	for _, name := range append(append([]string{}, metadata.Artists...), metadata.AlbumArtists...) {
		retained[normalizeIdentity(name)] = true
	}
	rows, err := store.database.QueryContext(ctx, `
		SELECT DISTINCT artists.id, artists.name, artists.name_normalized FROM artists
		WHERE artists.id IN (
			SELECT artist_id FROM track_artists WHERE track_id = ?
			UNION SELECT artist_id FROM album_artists WHERE album_id = ? AND ?
		) ORDER BY artists.name`, target.TrackID, target.AlbumID, removesAlbum)
	if err != nil {
		return nil, fmt.Errorf("list replacement Artist references: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, closeDeletionRows(rows, "replacement Artist references")) }()
	names = []string{}
	for rows.Next() {
		var artistID, name, normalized string
		if err := rows.Scan(&artistID, &name, &normalized); err != nil {
			return nil, fmt.Errorf("read replacement Artist reference: %w", err)
		}
		if retained[normalized] {
			continue
		}
		isReferenced, err := store.isArtistReferencedElsewhere(ctx, artistID, target, removesAlbum)
		if err != nil {
			return nil, err
		}
		if !isReferenced {
			names = append(names, name)
		}
	}
	return names, rows.Err()
}

func (store *Store) isArtistReferencedElsewhere(ctx context.Context, artistID string, target replacementTarget, removesAlbum bool) (bool, error) {
	var isReferenced bool
	err := store.database.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM track_artists WHERE artist_id = ? AND track_id != ?)
			OR EXISTS (SELECT 1 FROM album_artists WHERE artist_id = ? AND NOT (? AND album_id = ?))`,
		artistID, target.TrackID, artistID, removesAlbum, target.AlbumID).Scan(&isReferenced)
	if err != nil {
		return false, fmt.Errorf("inspect replacement Artist reference: %w", err)
	}
	return isReferenced, nil
}

func (store *Store) CreateReplacementJournal(ctx context.Context, journal replacementJournal) error {
	_, err := store.database.ExecContext(ctx, `
		INSERT INTO managed_track_replacements (
			id, job_id, track_id, phase, staged_file_path, pending_audio_path, audio_file_path,
			previous_audio_path, retired_audio_path, audio_sha256, previous_audio_sha256, artwork_mode,
			pending_artwork_path, artwork_file_path, previous_artwork_path, retired_artwork_path,
			artwork_sha256, previous_artwork_sha256, artwork_created, previous_album_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		journal.ID, journal.JobID, journal.TrackID, journal.Phase, journal.StagedFilePath, journal.PendingAudioPath,
		journal.AudioFilePath, journal.PreviousAudioPath, journal.RetiredAudioPath, journal.AudioSHA256,
		journal.PreviousAudioSHA256, journal.ArtworkMode, journal.PendingArtworkPath, journal.ArtworkFilePath,
		journal.PreviousArtworkPath, journal.RetiredArtworkPath, journal.ArtworkSHA256, journal.PreviousArtworkSHA256,
		journal.IsArtworkCreated, journal.PreviousAlbumID)
	if err != nil {
		return fmt.Errorf("create Track Replacement journal: %w", err)
	}
	return nil
}

func (store *Store) UpdateReplacementPhase(ctx context.Context, journalID string, from, to replacementPhase) error {
	result, err := store.database.ExecContext(ctx, `
		UPDATE managed_track_replacements SET phase = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND phase = ?`, to, journalID, from)
	if err != nil {
		return fmt.Errorf("journal Track Replacement phase %q: %w", to, err)
	}
	return requireMutation(result)
}

func (store *Store) MarkReplacementArtworkCreated(ctx context.Context, journalID string) error {
	result, err := store.database.ExecContext(ctx, `
		UPDATE managed_track_replacements SET artwork_created = 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND phase = ?`, journalID, REPLACEMENT_PHASE_PREPARED)
	if err != nil {
		return fmt.Errorf("journal Track Replacement artwork creation: %w", err)
	}
	return requireMutation(result)
}

func (store *Store) RollbackReplacementJournal(ctx context.Context, journalID, reason string) error {
	result, err := store.database.ExecContext(ctx, `
		UPDATE managed_track_replacements SET phase = ?, recovery_reason = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND phase NOT IN (?, ?, ?)`,
		REPLACEMENT_PHASE_ROLLED_BACK, reason, journalID, REPLACEMENT_PHASE_DATABASE_COMMITTED,
		REPLACEMENT_PHASE_COMPLETED, REPLACEMENT_PHASE_ROLLED_BACK)
	if err != nil {
		return fmt.Errorf("roll back Track Replacement journal: %w", err)
	}
	return requireMutation(result)
}

func (store *Store) RecordReplacementRecoveryReason(ctx context.Context, journalID, reason string) error {
	result, err := store.database.ExecContext(ctx, `
		UPDATE managed_track_replacements SET recovery_reason = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND phase NOT IN (?, ?)`, reason, journalID, REPLACEMENT_PHASE_COMPLETED, REPLACEMENT_PHASE_ROLLED_BACK)
	if err != nil {
		return fmt.Errorf("record Track Replacement recovery reason: %w", err)
	}
	return requireMutation(result)
}

const replacementJournalColumns = `id, job_id, track_id, phase, staged_file_path, pending_audio_path, audio_file_path,
	previous_audio_path, retired_audio_path, audio_sha256, previous_audio_sha256, artwork_mode,
	pending_artwork_path, artwork_file_path, previous_artwork_path, retired_artwork_path,
	artwork_sha256, previous_artwork_sha256, artwork_created, previous_album_id, COALESCE(recovery_reason, '')`

func scanReplacementJournal(scanner historyFileScanner) (replacementJournal, error) {
	var journal replacementJournal
	err := scanner.Scan(
		&journal.ID, &journal.JobID, &journal.TrackID, &journal.Phase, &journal.StagedFilePath, &journal.PendingAudioPath,
		&journal.AudioFilePath, &journal.PreviousAudioPath, &journal.RetiredAudioPath, &journal.AudioSHA256,
		&journal.PreviousAudioSHA256, &journal.ArtworkMode, &journal.PendingArtworkPath, &journal.ArtworkFilePath,
		&journal.PreviousArtworkPath, &journal.RetiredArtworkPath, &journal.ArtworkSHA256, &journal.PreviousArtworkSHA256,
		&journal.IsArtworkCreated, &journal.PreviousAlbumID, &journal.RecoveryReason,
	)
	if err != nil {
		return replacementJournal{}, err
	}
	return journal, nil
}

func (store *Store) ListIncompleteReplacementJournals(ctx context.Context) (journals []replacementJournal, returnErr error) {
	rows, err := store.database.QueryContext(ctx, `
		SELECT `+replacementJournalColumns+` FROM managed_track_replacements
		WHERE phase NOT IN (?, ?) ORDER BY created_at, id`, REPLACEMENT_PHASE_COMPLETED, REPLACEMENT_PHASE_ROLLED_BACK)
	if err != nil {
		return nil, fmt.Errorf("list incomplete Track Replacements: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	for rows.Next() {
		journal, err := scanReplacementJournal(rows)
		if err != nil {
			return nil, fmt.Errorf("read Track Replacement journal: %w", err)
		}
		journals = append(journals, journal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Track Replacement journals: %w", err)
	}
	return journals, nil
}

func (store *Store) FindIncompleteReplacementJournal(ctx context.Context, jobID string) (replacementJournal, bool, error) {
	journal, err := scanReplacementJournal(store.database.QueryRowContext(ctx, `
		SELECT `+replacementJournalColumns+` FROM managed_track_replacements
		WHERE job_id = ? AND phase NOT IN (?, ?)`, jobID, REPLACEMENT_PHASE_COMPLETED, REPLACEMENT_PHASE_ROLLED_BACK))
	if errors.Is(err, sql.ErrNoRows) {
		return replacementJournal{}, false, nil
	}
	if err != nil {
		return replacementJournal{}, false, fmt.Errorf("find Track Replacement journal: %w", err)
	}
	return journal, true, nil
}

func (store *Store) GetReplacementJournal(ctx context.Context, journalID string) (replacementJournal, error) {
	journal, err := scanReplacementJournal(store.database.QueryRowContext(ctx, `
		SELECT `+replacementJournalColumns+` FROM managed_track_replacements WHERE id = ?`, journalID))
	if errors.Is(err, sql.ErrNoRows) {
		return replacementJournal{}, ErrNotFound
	}
	if err != nil {
		return replacementJournal{}, fmt.Errorf("read Track Replacement journal: %w", err)
	}
	return journal, nil
}

func (store *Store) LatestReplacementJournalForJob(ctx context.Context, jobID string) (replacementJournal, error) {
	journal, err := scanReplacementJournal(store.database.QueryRowContext(ctx, `
		SELECT `+replacementJournalColumns+` FROM managed_track_replacements
		WHERE job_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, jobID))
	if errors.Is(err, sql.ErrNoRows) {
		return replacementJournal{}, ErrNotFound
	}
	if err != nil {
		return replacementJournal{}, fmt.Errorf("read latest Track Replacement journal: %w", err)
	}
	return journal, nil
}

// CommitReplacement makes the validated replacement metadata authoritative for the existing Track ID in one
// transaction and journals the database commit so a crash afterwards only has file cleanup left to finish.
func (store *Store) CommitReplacement(ctx context.Context, data replacementCommitData, journalID string) (invalidations []queueInvalidation, returnErr error) {
	transaction, beginErr := store.database.BeginTx(ctx, nil)
	if beginErr != nil {
		return nil, fmt.Errorf("begin Track Replacement commit: %w", beginErr)
	}
	defer func() {
		returnErr = errors.Join(returnErr, rollbackTransaction(transaction, "Track Replacement commit"))
	}()
	invalidations, err := writeReplacementData(ctx, transaction, data)
	if err != nil {
		return nil, err
	}
	result, err := transaction.ExecContext(ctx, `
		UPDATE managed_track_replacements SET phase = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND phase = ?`, REPLACEMENT_PHASE_DATABASE_COMMITTED, journalID, REPLACEMENT_PHASE_SWAPPED)
	if err != nil {
		return nil, fmt.Errorf("journal Track Replacement database commit: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit Track Replacement transaction: %w", err)
	}
	return invalidations, nil
}

func writeReplacementData(ctx context.Context, transaction *sql.Tx, data replacementCommitData) ([]queueInvalidation, error) {
	shell := commitData{Job: data.Job, Identity: data.Identity, Inspection: data.Inspection, AlbumKey: data.AlbumKey}
	shell.Placement = placedFiles{AudioPath: data.Placement.AudioPath, ArtworkPath: data.Placement.ArtworkPath}
	artistIDs, err := upsertArtists(ctx, transaction, data.Inspection.Metadata, data.Identity)
	if err != nil {
		return nil, err
	}
	if albumErr := upsertAlbum(ctx, transaction, shell, artistIDs); albumErr != nil {
		return nil, albumErr
	}
	previousArtistIDs, err := deletionArtistIDs(ctx, transaction, data.Target.TrackID, data.Target.AlbumID)
	if err != nil {
		return nil, err
	}
	isMovingAlbum := data.Identity.AlbumID != data.Target.AlbumID
	if isMovingAlbum {
		if err := moveOrDeleteArtworkReference(ctx, transaction, data.Target.TrackID, data.Target.AlbumID); err != nil {
			return nil, err
		}
	}
	if err := updateReplacedTrack(ctx, transaction, data); err != nil {
		return nil, err
	}
	if err := replaceTrackRelationships(ctx, transaction, data, artistIDs); err != nil {
		return nil, err
	}
	if err := writeReplacementArtwork(ctx, transaction, data, shell); err != nil {
		return nil, err
	}
	if isMovingAlbum {
		if err := deleteEmptyReplacementAlbum(ctx, transaction, data.Target.AlbumID); err != nil {
			return nil, err
		}
	}
	if err := deleteUnreferencedArtists(ctx, transaction, previousArtistIDs); err != nil {
		return nil, err
	}
	return advanceReplacementQueues(ctx, transaction, data.Target.Queues)
}

func updateReplacedTrack(ctx context.Context, transaction *sql.Tx, data replacementCommitData) error {
	metadata := data.Inspection.Metadata
	audio := data.Inspection.Audio
	fileInfo, err := os.Stat(data.Placement.AudioPath)
	if err != nil {
		return fmt.Errorf("stat replacement Managed Track: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
		UPDATE tracks SET album_id = ?, title = ?, title_sort = ?, artist_name = ?, track_no = ?, duration_ms = ?,
			format = ?, size_bytes = ?, file_path = ?, file_mtime = ?, genre = ?, sample_rate_hz = ?, bit_depth = ?,
			disc_no = ?, track_total = ?, disc_total = ?, channel_count = ?, bitrate_bps = ?, codec = ?, container = ?,
			replaygain_track_gain_db = ?, replaygain_track_peak = ?, replaygain_album_gain_db = ?,
			replaygain_album_peak = ?, identity_key = ?, revision = revision + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND revision = ? AND missing_at IS NULL`,
		data.Identity.AlbumID, metadata.Title, normalizeIdentity(metadata.Title), strings.Join(metadata.Artists, ", "),
		metadata.TrackPosition.Number, audio.DurationMs, audio.Format, fileInfo.Size(), data.Placement.AudioPath,
		fileInfo.ModTime().Unix(), metadata.Genres[0], audio.SampleRateHz, nullablePositive(audio.BitDepth),
		metadata.DiscPosition.Number, nullablePositive(metadata.TrackPosition.Total), nullablePositive(metadata.DiscPosition.Total),
		audio.ChannelCount, audio.BitrateKbps*BITS_PER_KILOBIT, audio.Codec, audio.Container,
		metadata.ReplayGain.TrackGainDB, metadata.ReplayGain.TrackPeak, metadata.ReplayGain.AlbumGainDB,
		metadata.ReplayGain.AlbumPeak, trackIdentityKey(metadata), data.Target.TrackID, data.Target.TrackRevision,
	)
	if err != nil {
		return fmt.Errorf("update replaced Managed Track: %w", err)
	}
	if mutationErr := requireMutation(result); mutationErr != nil {
		return fmt.Errorf("%w: Track changed during replacement", ErrReplacementConflict)
	}
	result, err = transaction.ExecContext(ctx, `
		UPDATE track_sources SET file_path = ?, content_sha256 = ?, source_format = ?, size_bytes = ?,
			revision = revision + 1, updated_at = CURRENT_TIMESTAMP
		WHERE track_id = ? AND revision = ? AND source_kind = 'managed'`,
		data.Placement.AudioPath, data.Inspection.FileSHA256, audio.Format, fileInfo.Size(),
		data.Target.TrackID, data.Target.SourceRevision)
	if err != nil {
		return fmt.Errorf("update replaced Managed Track source: %w", err)
	}
	if err := requireMutation(result); err != nil {
		return fmt.Errorf("%w: Track source changed during replacement", ErrReplacementConflict)
	}
	return nil
}

func replaceTrackRelationships(ctx context.Context, transaction *sql.Tx, data replacementCommitData, artistIDs map[string]string) error {
	trackID := data.Target.TrackID
	for _, query := range []string{`DELETE FROM track_artists WHERE track_id = ?`, `DELETE FROM track_genres WHERE track_id = ?`} {
		if _, err := transaction.ExecContext(ctx, query, trackID); err != nil {
			return fmt.Errorf("clear replaced Track relationships: %w", err)
		}
	}
	shell := commitData{Job: data.Job, Identity: data.Identity, Inspection: data.Inspection, AlbumKey: data.AlbumKey}
	return insertRelationships(ctx, transaction, shell, artistIDs)
}

func writeReplacementArtwork(ctx context.Context, transaction *sql.Tx, data replacementCommitData, shell commitData) error {
	switch data.Placement.ArtworkMode {
	case REPLACEMENT_ARTWORK_MODE_CREATE:
		shell.Identity.ExistingArtworkPath = ""
		return insertArtwork(ctx, transaction, shell)
	case REPLACEMENT_ARTWORK_MODE_REPLACE:
		artwork := data.Inspection.AlbumArtwork
		result, err := transaction.ExecContext(ctx, `
			UPDATE album_artwork SET source_track_id = ?, content_sha256 = ?, media_type = ?, width = ?, height = ?,
				encoded_size_bytes = ?, file_path = ?, revision = revision + 1, updated_at = CURRENT_TIMESTAMP
			WHERE album_id = ?`, data.Target.TrackID, artwork.SHA256, artwork.MIMEType, artwork.Width, artwork.Height,
			len(artwork.Data), data.Placement.ArtworkPath, data.Identity.AlbumID)
		if err != nil {
			return fmt.Errorf("replace Album Artwork: %w", err)
		}
		return requireMutation(result)
	default:
		return nil
	}
}

func deleteEmptyReplacementAlbum(ctx context.Context, transaction *sql.Tx, albumID string) error {
	if _, err := transaction.ExecContext(ctx, `DELETE FROM albums WHERE id = ? AND NOT EXISTS (SELECT 1 FROM tracks WHERE album_id = ?)`, albumID, albumID); err != nil {
		return fmt.Errorf("delete emptied Album after Track Replacement: %w", err)
	}
	return nil
}

func deleteUnreferencedArtists(ctx context.Context, transaction *sql.Tx, artistIDs []string) error {
	for _, artistID := range artistIDs {
		if _, err := transaction.ExecContext(ctx, `
			DELETE FROM artists WHERE id = ?
			AND NOT EXISTS (SELECT 1 FROM track_artists WHERE artist_id = ?)
			AND NOT EXISTS (SELECT 1 FROM album_artists WHERE artist_id = ?)`, artistID, artistID, artistID); err != nil {
			return fmt.Errorf("delete unreferenced Artist after Track Replacement: %w", err)
		}
	}
	return nil
}

func advanceReplacementQueues(ctx context.Context, transaction *sql.Tx, queues []TrackDeletionQueueReference) ([]queueInvalidation, error) {
	invalidations := make([]queueInvalidation, 0, len(queues))
	for _, queue := range queues {
		invalidation := queueInvalidation{userID: queue.UserID}
		if err := transaction.QueryRowContext(ctx, `
			INSERT INTO playback_queue_state (user_id, revision, event_sequence) VALUES (?, 1, 1)
			ON CONFLICT(user_id) DO UPDATE SET revision = revision + 1, event_sequence = event_sequence + 1
			RETURNING revision, event_sequence`, queue.UserID).Scan(&invalidation.revision, &invalidation.sequence); err != nil {
			return nil, fmt.Errorf("advance Queue revision after Track Replacement: %w", err)
		}
		invalidations = append(invalidations, invalidation)
	}
	return invalidations, nil
}

// FinalizeReplacement records the replaced outcome once the previous managed file has been deleted.
func (store *Store) FinalizeReplacement(ctx context.Context, job importJob, journalID string) (result TrackReplacementResult, returnErr error) {
	transaction, beginErr := store.database.BeginTx(ctx, nil)
	if beginErr != nil {
		return TrackReplacementResult{}, fmt.Errorf("begin Track Replacement finalization: %w", beginErr)
	}
	defer func() {
		returnErr = errors.Join(returnErr, rollbackTransaction(transaction, "Track Replacement finalization"))
	}()
	jobResult, err := transaction.ExecContext(ctx, `
		UPDATE managed_import_jobs
		SET status = ?, revision = revision + 1, track_id = replace_track_id, outcome = ?, staged_file_path = NULL,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = ? AND revision = ?`,
		STATUS_COMMITTED, OUTCOME_REPLACED, job.ID, STATUS_AWAITING_CONFIRMATION, job.Revision)
	if err != nil {
		return TrackReplacementResult{}, fmt.Errorf("mark Track Replacement committed: %w", err)
	}
	if mutationErr := requireMutation(jobResult); mutationErr != nil {
		return TrackReplacementResult{}, mutationErr
	}
	journalResult, err := transaction.ExecContext(ctx, `
		UPDATE managed_track_replacements
		SET phase = ?, recovery_reason = COALESCE(recovery_reason, 'replacement completed'), updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND phase = ?`, REPLACEMENT_PHASE_COMPLETED, journalID, REPLACEMENT_PHASE_DATABASE_COMMITTED)
	if err != nil {
		return TrackReplacementResult{}, fmt.Errorf("complete Track Replacement journal: %w", err)
	}
	if err := requireMutation(journalResult); err != nil {
		return TrackReplacementResult{}, err
	}
	if err := archiveStandaloneHistory(ctx, transaction, job.ID, HISTORY_RESULT_COMPLETED); err != nil {
		return TrackReplacementResult{}, err
	}
	if err := transaction.Commit(); err != nil {
		return TrackReplacementResult{}, fmt.Errorf("commit Track Replacement finalization: %w", err)
	}
	return TrackReplacementResult{JobID: job.ID, Status: STATUS_COMMITTED, Revision: job.Revision + 1, TrackID: job.ReplaceTrackID}, nil
}

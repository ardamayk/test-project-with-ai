package library

import (
	"context"
	"log/slog"
	"sync"

	"github.com/ardam/navidrome-replacement/server/internal/config"
)

type Service struct {
	store      *Store
	musicPaths []string
	mu         sync.Mutex
}

func NewService(store *Store, cfg config.Config) *Service {
	return &Service{
		store:      store,
		musicPaths: cfg.MusicPaths,
	}
}

func (s *Service) MusicPathsConfigured() bool {
	return len(s.musicPaths) > 0
}

func (s *Service) TriggerScan(ctx context.Context) (ScanStatus, error) {
	if !s.MusicPathsConfigured() {
		return ScanStatus{}, ErrNoMusicPaths
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	jobID, err := s.store.BeginScan(ctx)
	if err != nil {
		return ScanStatus{}, err
	}

	go s.runScan(jobID)

	st, err := s.store.GetScanStatus(ctx)
	if err != nil {
		return ScanStatus{Status: "running"}, nil
	}
	return st, nil
}

func (s *Service) runScan(jobID string) {
	ctx := context.Background()
	scanned, added, updated := 0, 0, 0
	seenPaths := make(map[string]struct{})

	files, err := WalkMusicPaths(s.musicPaths)
	if err != nil {
		slog.Error("library scan walk failed", "error", err, "jobId", jobID)
		_ = s.store.FinishScan(ctx, jobID, "failed", err.Error(), scanned, added, updated, 0)
		return
	}

	for _, file := range files {
		scanned++
		seenPaths[file.Metadata.Path] = struct{}{}
		isAdded, isUpdated, err := s.store.UpsertFromScan(ctx, file.Metadata)
		if err != nil {
			slog.Error("library scan upsert failed", "path", file.Metadata.Path, "error", err)
			continue
		}
		if isAdded {
			added++
		}
		if isUpdated {
			updated++
		}
		_ = s.store.UpdateScanProgress(ctx, jobID, scanned, added, updated, 0)
	}

	removed, err := s.store.MarkSeenPaths(ctx, seenPaths)
	if err != nil {
		slog.Error("library scan mark missing failed", "error", err)
		_ = s.store.FinishScan(ctx, jobID, "failed", err.Error(), scanned, added, updated, 0)
		return
	}

	_ = s.store.UpdateScanProgress(ctx, jobID, scanned, added, updated, removed)
	_ = s.store.FinishScan(ctx, jobID, "completed", "", scanned, added, updated, removed)
	slog.Info("library scan completed", "jobId", jobID, "scanned", scanned, "added", added, "updated", updated, "removed", removed)
}

func (s *Service) GetScanStatus(ctx context.Context) (ScanStatus, error) {
	return s.store.GetScanStatus(ctx)
}

func (s *Service) ListArtists(ctx context.Context, limit, offset int, q string) (ArtistList, error) {
	return s.store.ListArtists(ctx, limit, offset, q)
}

func (s *Service) ListAlbums(ctx context.Context, limit, offset int, artistID, q string) (AlbumList, error) {
	return s.store.ListAlbums(ctx, limit, offset, artistID, q)
}

func (s *Service) GetAlbum(ctx context.Context, albumID string) (AlbumDetail, error) {
	return s.store.GetAlbum(ctx, albumID)
}

func (s *Service) ListTracks(ctx context.Context, limit, offset int, q string) (TrackList, error) {
	return s.store.ListTracks(ctx, limit, offset, q)
}

func (s *Service) GetTrack(ctx context.Context, trackID string) (Track, error) {
	return s.store.GetTrack(ctx, trackID)
}

func (s *Service) GetTrackFilePath(ctx context.Context, trackID string) (string, error) {
	return s.store.GetTrackFilePath(ctx, trackID)
}

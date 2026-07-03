package library

import (
	"context"
	"sync"

	"github.com/ardam/navidrome-replacement/server/internal/config"
)

type Service struct {
	store      *Store
	scanRunner *ScanRunner
	mu         sync.Mutex
}

func NewService(store *Store, cfg config.Config) *Service {
	return &Service{
		store:      store,
		scanRunner: NewScanRunner(store, cfg.MusicPaths),
	}
}

func (s *Service) MusicPathsConfigured() bool {
	return s.scanRunner.HasMusicPaths()
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

	go s.scanRunner.Run(jobID)

	st, err := s.store.GetScanStatus(ctx)
	if err != nil {
		return ScanStatus{Status: "running"}, nil
	}
	return st, nil
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

func (s *Service) GetAlbumCover(ctx context.Context, albumID string) (string, []byte, error) {
	return s.store.GetAlbumCover(ctx, albumID)
}

func (s *Service) GetTrack(ctx context.Context, trackID string) (Track, error) {
	return s.store.GetTrack(ctx, trackID)
}

func (s *Service) GetTrackFilePath(ctx context.Context, trackID string) (string, error) {
	return s.store.GetTrackFilePath(ctx, trackID)
}

func (s *Service) DeleteTrack(ctx context.Context, trackID string) (DeleteResult, error) {
	return s.store.DeleteTrack(ctx, trackID, func(path string) error {
		return removeMusicFile(path, s.scanRunner.musicPaths)
	})
}

func (s *Service) DeleteAlbum(ctx context.Context, albumID string) (DeleteResult, error) {
	return s.store.DeleteAlbum(ctx, albumID, func(path string) error {
		return removeMusicFile(path, s.scanRunner.musicPaths)
	})
}

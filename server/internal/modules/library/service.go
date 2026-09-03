package library

import (
	"context"

	"github.com/ardam/navidrome-replacement/server/internal/config"
)

// Service exposes read and delete operations over the library. Ingestion is
// owned by Managed Import; the retired legacy scanner never runs here.
type Service struct {
	store      *Store
	musicPaths []string
}

func NewService(store *Store, cfg config.Config) *Service {
	return &Service{
		store:      store,
		musicPaths: cfg.MusicPaths,
	}
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
		return removeMusicFile(path, s.musicPaths)
	})
}

func (s *Service) DeleteAlbum(ctx context.Context, albumID string) (DeleteResult, error) {
	return s.store.DeleteAlbum(ctx, albumID, func(path string) error {
		return removeMusicFile(path, s.musicPaths)
	})
}

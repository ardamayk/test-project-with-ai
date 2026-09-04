package library

import "context"

// Service exposes read operations over the library. Ingestion, deletion and
// replacement are owned by Managed Import.
type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
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

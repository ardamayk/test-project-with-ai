package library

import "context"

type TrackReader interface {
	GetTrack(ctx context.Context, trackID string) (Track, error)
}

type TrackAccess interface {
	TrackReader
	GetTrackFilePath(ctx context.Context, trackID string) (string, error)
}

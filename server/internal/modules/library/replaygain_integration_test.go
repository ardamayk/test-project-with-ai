package library

import (
	"context"
	"testing"
	"time"
)

func TestStorePreservesAndRefreshesReplayGainMetadata(t *testing.T) {
	db := openMemoryDB(t)
	store := NewStore(db)
	ctx := context.Background()
	modTime := time.Unix(1_700_000_000, 0)
	meta := FileMetadata{
		Path:        "/music/artist/album/song.flac",
		Format:      "flac",
		SizeBytes:   1024,
		ModTime:     modTime,
		Title:       "Song",
		Artist:      "Artist",
		AlbumArtist: "Artist",
		Album:       "Album",
		Genre:       "Rock",
		ReplayGain: ReplayGainMetadata{
			TrackGainDB: float64Pointer(-7.25),
			TrackPeak:   float64Pointer(0.98),
			AlbumGainDB: float64Pointer(-6.5),
			AlbumPeak:   float64Pointer(1.01),
		},
	}

	added, updated, err := store.SeedLegacyTrack(ctx, meta)
	if err != nil {
		t.Fatal(err)
	}
	if !added || updated {
		t.Fatalf("upsert flags = (%v, %v), want (true, false)", added, updated)
	}

	tracks, err := store.ListTracks(ctx, 10, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks.Items) != 1 {
		t.Fatalf("tracks = %d, want 1", len(tracks.Items))
	}
	trackID := tracks.Items[0].ID
	assertReplayGainMetadata(t, tracks.Items[0].ReplayGain, meta.ReplayGain)

	meta.ModTime = modTime.Add(time.Second)
	meta.Genre = ""
	meta.ReplayGain = ReplayGainMetadata{TrackGainDB: float64Pointer(-5.75)}
	added, updated, err = store.SeedLegacyTrack(ctx, meta)
	if err != nil {
		t.Fatal(err)
	}
	if added || !updated {
		t.Fatalf("upsert flags = (%v, %v), want (false, true)", added, updated)
	}

	track, err := store.GetTrack(ctx, trackID)
	if err != nil {
		t.Fatal(err)
	}
	if track.ID != trackID || track.Title != "Song" || track.Genre != "Rock" {
		t.Fatalf("unrelated track data changed: %+v", track)
	}
	assertReplayGainMetadata(t, track.ReplayGain, meta.ReplayGain)
}

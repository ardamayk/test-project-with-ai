package library

import (
	"context"
	"log/slog"
)

type ScanRunner struct {
	store      *Store
	musicPaths []string
}

func NewScanRunner(store *Store, musicPaths []string) *ScanRunner {
	return &ScanRunner{store: store, musicPaths: musicPaths}
}

func (r *ScanRunner) HasMusicPaths() bool {
	return len(r.musicPaths) > 0
}

func (r *ScanRunner) Run(jobID string) {
	ctx := context.Background()
	scanned, added, updated := 0, 0, 0
	seenPaths := make(map[string]struct{})

	files, err := WalkMusicPaths(r.musicPaths)
	if err != nil {
		slog.Error("library scan walk failed", "error", err, "jobId", jobID)
		_ = r.store.FinishScan(ctx, jobID, "failed", err.Error(), scanned, added, updated, 0)
		return
	}

	for _, file := range files {
		scanned++
		seenPaths[file.Metadata.Path] = struct{}{}
		isAdded, isUpdated, err := r.store.UpsertFromScan(ctx, file.Metadata)
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
		_ = r.store.UpdateScanProgress(ctx, jobID, scanned, added, updated, 0)
	}

	removed, err := r.store.MarkSeenPaths(ctx, seenPaths)
	if err != nil {
		slog.Error("library scan mark missing failed", "error", err)
		_ = r.store.FinishScan(ctx, jobID, "failed", err.Error(), scanned, added, updated, 0)
		return
	}

	_ = r.store.UpdateScanProgress(ctx, jobID, scanned, added, updated, removed)
	if err := r.store.RecomputeAllAlbumGenres(ctx); err != nil {
		slog.Error("library scan recompute album genres failed", "error", err, "jobId", jobID)
	}
	_ = r.store.FinishScan(ctx, jobID, "completed", "", scanned, added, updated, removed)
	slog.Info("library scan completed", "jobId", jobID, "scanned", scanned, "added", added, "updated", updated, "removed", removed)
}

package managedimport

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestStageUploadProbesCapacityAtIntervalsNotPerChunk(t *testing.T) {
	probes := 0
	storage := newStorage(t.TempDir(), StorageLimits{ReserveBytes: 1024, FileBytes: 64 << 20, BatchBytes: 64 << 20}, func(string) (int64, error) {
		probes++
		return 1 << 40, nil
	})
	const uploadSize = 10 << 20
	upload, err := storage.StageUpload(io.LimitReader(zeroReader{}, uploadSize), uploadSize)
	if err != nil {
		t.Fatalf("StageUpload() error = %v", err)
	}
	if upload.Size != uploadSize {
		t.Fatalf("staged size = %d, want %d", upload.Size, uploadSize)
	}
	// One preflight before writing, one probe on the first chunk, then one per
	// CAPACITY_CHECK_INTERVAL_BYTES boundary (at 4 MiB and 8 MiB).
	const wantProbes = 2 + uploadSize/CAPACITY_CHECK_INTERVAL_BYTES
	if probes != wantProbes {
		t.Fatalf("capacity probes = %d, want %d", probes, wantProbes)
	}
}

func TestCapacityWriterProbesForTheNextIntervalNotTheCurrentChunk(t *testing.T) {
	// A full interval is requested while more than an interval remains, only
	// the remainder near the end (so a file that exactly fits is accepted), and
	// a full interval again when the upload length is unknown.
	writer := &capacityWriter{remainingBytes: 6 << 20}
	if got := writer.nextProbeBytes(32 << 10); got != CAPACITY_CHECK_INTERVAL_BYTES {
		t.Fatalf("first probe bytes = %d, want %d", got, CAPACITY_CHECK_INTERVAL_BYTES)
	}
	writer.remainingBytes = 1 << 20
	if got := writer.nextProbeBytes(32 << 10); got != 1<<20 {
		t.Fatalf("tail probe bytes = %d, want %d", got, 1<<20)
	}
	writer.remainingBytes = -1
	if got := writer.nextProbeBytes(32 << 10); got != CAPACITY_CHECK_INTERVAL_BYTES {
		t.Fatalf("unknown-length probe bytes = %d, want %d", got, CAPACITY_CHECK_INTERVAL_BYTES)
	}
}

func TestStageUploadStopsWhenCapacityRunsOutMidStream(t *testing.T) {
	probes := 0
	storage := newStorage(t.TempDir(), StorageLimits{ReserveBytes: 1024, FileBytes: 64 << 20, BatchBytes: 64 << 20}, func(string) (int64, error) {
		probes++
		// Probe 1 is the preflight, probe 2 the first chunk; probe 3 fires at
		// the 4 MiB boundary while bytes are still streaming.
		if probes > 2 {
			return 0, nil
		}
		return 1 << 40, nil
	})
	const uploadSize = 10 << 20
	_, err := storage.StageUpload(bytes.NewReader(make([]byte, uploadSize)), uploadSize)
	if !errors.Is(err, ErrInsufficientStorage) {
		t.Fatalf("StageUpload() error = %v, want ErrInsufficientStorage", err)
	}
	if probes != 3 {
		t.Fatalf("capacity probes = %d, want 3", probes)
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

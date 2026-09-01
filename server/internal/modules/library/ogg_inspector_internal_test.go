package library

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeOGGStreamRejectsOversizedHeaderPackets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized-header.ogg")
	if err := os.WriteFile(path, buildOversizedOGGHeaderFixture(), 0o600); err != nil {
		t.Fatalf("write oversized OGG fixture: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open oversized OGG fixture: %v", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Errorf("close OGG fixture: %v", closeErr)
		}
	}()

	if _, err = analyzeOGGStream(context.Background(), file, 3); err == nil || !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("oversized header packet error = %v", err)
	}
}

func TestAnalyzeOGGStreamHonorsCancellation(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "strict-import.ogg"))
	if err != nil {
		t.Fatalf("read strict OGG fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "cancelled.ogg")
	if err = os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatalf("write OGG fixture: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open OGG fixture: %v", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Errorf("close OGG fixture: %v", closeErr)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = analyzeOGGStream(ctx, file, 3); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled analysis error = %v", err)
	}
}

func buildOversizedOGGHeaderFixture() []byte {
	const serial = uint32(0x5eed_1234)
	var fixture []byte
	fixture = append(fixture, buildSyntheticOGGPage(OGG_BEGINNING_OF_STREAM_FLAG, 0, serial, []byte{4}, []byte{1, 2, 3, 4})...)
	lacing := make([]byte, 255)
	for index := range lacing {
		lacing[index] = OGG_MAX_SEGMENT_SIZE_BYTES
	}
	payload := make([]byte, len(lacing)*int(OGG_MAX_SEGMENT_SIZE_BYTES))
	for total, sequence := 0, uint32(1); int64(total) <= MAX_OGG_HEADER_PACKET_BYTES; total, sequence = total+len(payload), sequence+1 {
		headerType := byte(0)
		if total > 0 {
			headerType = OGG_CONTINUED_PACKET_FLAG
		}
		fixture = append(fixture, buildSyntheticOGGPage(headerType, sequence, serial, lacing, payload)...)
	}
	return fixture
}

func buildSyntheticOGGPage(headerType byte, sequence, serial uint32, lacing, payload []byte) []byte {
	page := make([]byte, OGG_PAGE_HEADER_SIZE_BYTES, OGG_PAGE_HEADER_SIZE_BYTES+len(lacing)+len(payload))
	copy(page, OGG_SIGNATURE)
	page[5] = headerType
	binary.LittleEndian.PutUint32(page[14:], serial)
	binary.LittleEndian.PutUint32(page[18:], sequence)
	page[26] = byte(len(lacing))
	page = append(page, lacing...)
	page = append(page, payload...)
	checksum := updateOGGChecksum(0, page[:OGG_PAGE_HEADER_SIZE_BYTES])
	checksum = updateOGGChecksum(checksum, page[OGG_PAGE_HEADER_SIZE_BYTES:OGG_PAGE_HEADER_SIZE_BYTES+len(lacing)])
	checksum = updateOGGChecksum(checksum, page[OGG_PAGE_HEADER_SIZE_BYTES+len(lacing):])
	binary.LittleEndian.PutUint32(page[22:], checksum)
	return page
}

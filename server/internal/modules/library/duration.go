package library

import (
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

func durationMsFromFile(path, format string, m tag.Metadata) int {
	if format == "flac" {
		if ms := flacDurationMs(path); ms > 0 {
			return ms
		}
	}
	if ms := id3DurationMs(m); ms > 0 {
		return ms
	}
	return 0
}

func id3DurationMs(m tag.Metadata) int {
	raw := m.Raw()
	if raw == nil {
		return 0
	}
	for _, key := range []string{"TLEN", "Length"} {
		v, ok := raw[key]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case string:
			ms, err := strconv.Atoi(strings.TrimSpace(n))
			if err == nil && ms > 0 {
				return ms
			}
		case int:
			if n > 0 {
				return n
			}
		case int64:
			if n > 0 {
				return int(n)
			}
		}
	}
	return 0
}

func flacDurationMs(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	var header [4]byte
	if _, err := io.ReadFull(f, header[:]); err != nil || string(header[:]) != "fLaC" {
		return 0
	}

	for {
		var blockHeader [4]byte
		if _, err := io.ReadFull(f, blockHeader[:]); err != nil {
			return 0
		}
		blockType := blockHeader[0] & 0x7F
		isLast := blockHeader[0]&0x80 != 0
		size := int(blockHeader[1])<<16 | int(blockHeader[2])<<8 | int(blockHeader[3])

		if blockType == 0 {
			if size < 18 {
				return 0
			}
			data := make([]byte, size)
			if _, err := io.ReadFull(f, data); err != nil {
				return 0
			}
			sampleRate := (uint32(data[10]) << 12) | (uint32(data[11]) << 4) | (uint32(data[12]) >> 4)
			totalSamples := (uint64(data[13]&0x0F) << 32) | (uint64(data[14]) << 24) |
				(uint64(data[15]) << 16) | (uint64(data[16]) << 8) | uint64(data[17])
			if sampleRate == 0 {
				return 0
			}
			return int(totalSamples * 1000 / uint64(sampleRate))
		}

		if _, err := f.Seek(int64(size), io.SeekCurrent); err != nil {
			return 0
		}
		if isLast {
			break
		}
	}
	return 0
}

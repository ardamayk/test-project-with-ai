package library

import (
	"io"
	"os"
)

type flacStreamInfo struct {
	SampleRateHz  int
	BitsPerSample int
	DurationMs    int
}

func readFlacStreamInfo(path string) (flacStreamInfo, bool) {
	f, err := os.Open(path)
	if err != nil {
		return flacStreamInfo{}, false
	}
	defer func() { _ = f.Close() }()

	var header [4]byte
	if _, err := io.ReadFull(f, header[:]); err != nil || string(header[:]) != "fLaC" {
		return flacStreamInfo{}, false
	}

	for {
		var blockHeader [4]byte
		if _, err := io.ReadFull(f, blockHeader[:]); err != nil {
			return flacStreamInfo{}, false
		}
		blockType := blockHeader[0] & 0x7F
		isLast := blockHeader[0]&0x80 != 0
		size := int(blockHeader[1])<<16 | int(blockHeader[2])<<8 | int(blockHeader[3])

		if blockType == 0 {
			if size < 18 {
				return flacStreamInfo{}, false
			}
			data := make([]byte, size)
			if _, err := io.ReadFull(f, data); err != nil {
				return flacStreamInfo{}, false
			}

			sampleRate := (uint32(data[10]) << 12) | (uint32(data[11]) << 4) | (uint32(data[12]) >> 4)
			bitsMinus1 := ((int(data[12]) & 0x01) << 4) | (int(data[13]) >> 4)
			totalSamples := (uint64(data[13]&0x0F) << 32) | (uint64(data[14]) << 24) |
				(uint64(data[15]) << 16) | (uint64(data[16]) << 8) | uint64(data[17])
			if sampleRate == 0 {
				return flacStreamInfo{}, false
			}

			info := flacStreamInfo{
				SampleRateHz:  int(sampleRate),
				BitsPerSample: bitsMinus1 + 1,
			}
			if totalSamples > 0 {
				info.DurationMs = int(totalSamples * 1000 / uint64(sampleRate))
			}
			return info, true
		}

		if _, err := f.Seek(int64(size), io.SeekCurrent); err != nil {
			return flacStreamInfo{}, false
		}
		if isLast {
			break
		}
	}

	return flacStreamInfo{}, false
}

func applyAudioFormat(meta *FileMetadata) {
	if meta.Format == "flac" {
		if info, ok := readFlacStreamInfo(meta.Path); ok {
			if info.SampleRateHz > 0 {
				meta.SampleRateHz = info.SampleRateHz
			}
			if info.BitsPerSample > 0 {
				meta.BitDepth = info.BitsPerSample
			}
			if meta.DurationMs == 0 && info.DurationMs > 0 {
				meta.DurationMs = info.DurationMs
			}
		}
	}
}

func estimateBitrateKbps(sizeBytes int64, durationMs int) int {
	if sizeBytes <= 0 || durationMs <= 0 {
		return 0
	}
	kbps := int((sizeBytes * 8) / int64(durationMs))
	if kbps <= 0 {
		return 0
	}
	return kbps
}

func enrichTrackBitrate(track *Track) {
	if track.BitDepth > 0 || track.SampleRateHz > 0 {
		return
	}
	if kbps := estimateBitrateKbps(track.SizeBytes, track.DurationMs); kbps > 0 {
		track.BitrateKbps = kbps
	}
}

package library

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// parseWAVID3Chunk adapts the shared strict MP3 ID3 parser to a bounded RIFF chunk.
func parseWAVID3Chunk(file *os.File, chunkSize int64) (map[string][]string, []taggedID3Picture, error) {
	if chunkSize < ID3_HEADER_SIZE_BYTES || chunkSize > MAX_ID3_TAG_SIZE_BYTES+ID3_HEADER_SIZE_BYTES {
		return nil, nil, invalidID3Error(fmt.Errorf("ID3 chunk size %d is invalid", chunkSize))
	}
	body := make([]byte, chunkSize)
	if _, err := io.ReadFull(file, body); err != nil {
		return nil, nil, inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("read ID3 chunk: %w", err))
	}
	version, payload, err := parseWAVID3Header(body)
	if err != nil {
		return nil, nil, err
	}
	frames, err := parseID3Frames(payload, version)
	if err != nil {
		return nil, nil, err
	}
	values, err := collectID3Values(frames, version)
	if err != nil {
		return nil, nil, err
	}
	return values.tags, values.pictures, nil
}

func parseWAVID3Header(body []byte) (byte, []byte, error) {
	if string(body[:ID3_SIGNATURE_SIZE_BYTES]) != ID3_SIGNATURE {
		return 0, nil, invalidID3Error(errors.New("ID3 signature is missing"))
	}
	version := body[ID3_MAJOR_VERSION_OFFSET]
	if version < ID3_VERSION_2 || version > ID3_VERSION_4 || body[ID3_REVISION_OFFSET] != 0 {
		return 0, nil, invalidID3Error(fmt.Errorf("unsupported ID3v2 version %d.%d", version, body[ID3_REVISION_OFFSET]))
	}
	if body[ID3_FLAGS_OFFSET] != 0 {
		return 0, nil, invalidID3Error(errors.New("ID3 tag flags are not supported by the Strict Import Profile"))
	}
	size, err := decodeSyncSafeInt(body[ID3_SIZE_OFFSET:ID3_HEADER_SIZE_BYTES])
	if err != nil || size <= 0 || size > MAX_ID3_TAG_SIZE_BYTES || ID3_HEADER_SIZE_BYTES+size != len(body) {
		return 0, nil, invalidID3Error(errors.New("ID3 tag size is invalid"))
	}
	return version, body[ID3_HEADER_SIZE_BYTES:], nil
}

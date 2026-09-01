package library

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// WAV (RIFF WAVE) support follows the same Strict Import Profile as FLAC:
// every identity tag and one embedded front cover must parse, the PCM format
// must be well-formed, and the complete data chunk must decode to EOF.

const (
	WAVE_FORMAT_PCM        uint16 = 0x0001
	WAVE_FORMAT_EXTENSIBLE uint16 = 0xFFFE

	RIFF_HEADER_SIZE_BYTES          = 12
	RIFF_CHUNK_HEADER_SIZE_BYTES    = 8
	WAV_FMT_MIN_SIZE_BYTES          = 16
	WAV_FMT_WAVEFORMATEX_SIZE_BYTES = 18
	WAV_FMT_EXTENSIBLE_SIZE_BYTES   = 40
	WAV_MAX_CHANNELS                = 8
	WAV_MIN_SAMPLE_RATE_HZ          = 8000
	WAV_MAX_SAMPLE_RATE_HZ          = 384000
	WAV_MIN_BIT_DEPTH               = 8
	WAV_MAX_BIT_DEPTH               = 32
)

var wavPCMSubFormat = [16]byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}

type wavInspectionInput struct {
	file           *os.File
	reportProgress InspectionProgressReporter
	fileHash       string
	sizeBytes      int64
}

type wavDataChunk struct {
	offset int64
	size   int64
}

type wavFormatChunk struct {
	audioFormat    uint16
	channelCount   uint16
	sampleRateHz   uint32
	bytesPerSecond uint32
	blockAlign     uint16
	bitsPerSample  uint16
	cbSize         uint16
	validBits      uint16
	subFormat      [16]byte
}

func inspectOpenWAV(ctx context.Context, file *os.File, reportProgress InspectionProgressReporter) (MediaInspection, error) {
	fileHash, sizeBytes, err := hashAndRewind(ctx, file)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return MediaInspection{}, inspectionError(INSPECTION_ERROR_VALIDATION_CANCELLED, "validation", err)
		}
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", err)
	}
	if err := validateWAVHeader(file, sizeBytes); err != nil {
		return MediaInspection{}, err
	}
	input := wavInspectionInput{file: file, reportProgress: reportProgress, fileHash: fileHash, sizeBytes: sizeBytes}
	return input.inspect(ctx)
}

func validateWAVHeader(file *os.File, sizeBytes int64) error {
	var header [RIFF_HEADER_SIZE_BYTES]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		return inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("read WAV header: %w", err))
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("rewind WAV header: %w", err))
	}
	declaredSize := int64(binary.LittleEndian.Uint32(header[4:8]))
	if declaredSize+RIFF_HEADER_SIZE_BYTES-4 != sizeBytes {
		return inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", fmt.Errorf("RIFF size %d does not match the file size %d", declaredSize, sizeBytes))
	}
	return nil
}

func (input wavInspectionInput) inspect(ctx context.Context) (MediaInspection, error) {
	format, data, tags, artwork, err := input.readChunks()
	if err != nil {
		return MediaInspection{}, err
	}
	metadata, err := normalizeMediaMetadata(tags, ReplayGainMetadata{})
	if err != nil {
		return MediaInspection{}, err
	}
	embeddedArtwork, err := inspectWAVArtwork(artwork)
	if err != nil {
		return MediaInspection{}, err
	}
	audio, err := input.decodePCM(ctx, format, data)
	if err != nil {
		return MediaInspection{}, err
	}
	return MediaInspection{Metadata: metadata, AlbumArtwork: embeddedArtwork, Audio: audio, FileSHA256: input.fileHash}, nil
}

// readChunks walks every RIFF chunk once. Trailing bytes, overlapping chunk
// bounds, or a missing fmt/data chunk reject the file before any decoding.
func (input wavInspectionInput) readChunks() (*wavFormatChunk, wavDataChunk, map[string][]string, *id3AttachedPicture, error) {
	if _, err := input.file.Seek(RIFF_HEADER_SIZE_BYTES, io.SeekStart); err != nil {
		return nil, wavDataChunk{}, nil, nil, inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("seek past WAV header: %w", err))
	}
	var format *wavFormatChunk
	var data wavDataChunk
	hasData := false
	var tags map[string][]string
	var artwork *id3AttachedPicture
	offset := int64(RIFF_HEADER_SIZE_BYTES)
	for {
		var header [RIFF_CHUNK_HEADER_SIZE_BYTES]byte
		if _, err := io.ReadFull(input.file, header[:]); err != nil {
			return nil, wavDataChunk{}, nil, nil, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", fmt.Errorf("read RIFF chunk header: %w", err))
		}
		chunkID := string(header[:4])
		chunkSize := int64(binary.LittleEndian.Uint32(header[4:]))
		bodyOffset := offset + RIFF_CHUNK_HEADER_SIZE_BYTES
		if chunkSize < 0 || bodyOffset+chunkSize > input.sizeBytes {
			return nil, wavDataChunk{}, nil, nil, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", fmt.Errorf("RIFF chunk %q extends past the file", chunkID))
		}
		switch chunkID {
		case "fmt ":
			parsedFormat, err := readWAVFormatChunk(input.file, chunkSize)
			if err != nil {
				return nil, wavDataChunk{}, nil, nil, err
			}
			if format != nil {
				return nil, wavDataChunk{}, nil, nil, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", errors.New("multiple fmt chunks are ambiguous"))
			}
			format = parsedFormat
		case "data":
			if hasData {
				return nil, wavDataChunk{}, nil, nil, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", errors.New("multiple data chunks are ambiguous"))
			}
			hasData = true
			data = wavDataChunk{offset: bodyOffset, size: chunkSize}
		case "ID3 ", "id3 ":
			parsedTags, parsedArtwork, err := parseID3v2Chunk(input.file, chunkSize)
			if err != nil {
				return nil, wavDataChunk{}, nil, nil, err
			}
			if tags != nil {
				return nil, wavDataChunk{}, nil, nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("multiple ID3 chunks are ambiguous"))
			}
			tags = parsedTags
			artwork = parsedArtwork
		}
		nextOffset := bodyOffset + chunkSize
		if chunkSize%2 == 1 {
			if nextOffset >= input.sizeBytes {
				return nil, wavDataChunk{}, nil, nil, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", fmt.Errorf("RIFF chunk %q is missing its padding byte", chunkID))
			}
			nextOffset++
		}
		if nextOffset >= input.sizeBytes {
			break
		}
		if _, err := input.file.Seek(nextOffset, io.SeekStart); err != nil {
			return nil, wavDataChunk{}, nil, nil, inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("seek RIFF chunk: %w", err))
		}
		offset = nextOffset
	}
	if format == nil {
		return nil, wavDataChunk{}, nil, nil, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", errors.New("WAV fmt chunk is missing"))
	}
	if data.size <= 0 {
		return nil, wavDataChunk{}, nil, nil, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("WAV data chunk is missing or empty"))
	}
	if tags == nil {
		return nil, wavDataChunk{}, nil, nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("WAV ID3 chunk is missing; identity metadata is required"))
	}
	return format, data, tags, artwork, nil
}

func readWAVFormatChunk(file *os.File, chunkSize int64) (*wavFormatChunk, error) {
	if chunkSize < WAV_FMT_MIN_SIZE_BYTES || chunkSize > WAV_FMT_EXTENSIBLE_SIZE_BYTES {
		return nil, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", fmt.Errorf("fmt chunk size %d is invalid", chunkSize))
	}
	body := make([]byte, chunkSize)
	if _, err := io.ReadFull(file, body); err != nil {
		return nil, inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("read fmt chunk: %w", err))
	}
	format := &wavFormatChunk{
		audioFormat:    binary.LittleEndian.Uint16(body[0:2]),
		channelCount:   binary.LittleEndian.Uint16(body[2:4]),
		sampleRateHz:   binary.LittleEndian.Uint32(body[4:8]),
		bytesPerSecond: binary.LittleEndian.Uint32(body[8:12]),
		blockAlign:     binary.LittleEndian.Uint16(body[12:14]),
		bitsPerSample:  binary.LittleEndian.Uint16(body[14:16]),
	}
	if chunkSize >= WAV_FMT_WAVEFORMATEX_SIZE_BYTES {
		format.cbSize = binary.LittleEndian.Uint16(body[16:18])
	}
	if format.audioFormat == WAVE_FORMAT_EXTENSIBLE {
		if chunkSize < WAV_FMT_EXTENSIBLE_SIZE_BYTES {
			return nil, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", errors.New("extensible fmt chunk is truncated"))
		}
		format.validBits = binary.LittleEndian.Uint16(body[18:20])
		copy(format.subFormat[:], body[24:40])
	} else if chunkSize != WAV_FMT_MIN_SIZE_BYTES && (chunkSize != WAV_FMT_WAVEFORMATEX_SIZE_BYTES || format.cbSize != 0) {
		return nil, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", fmt.Errorf("PCM fmt chunk size %d is invalid", chunkSize))
	}
	if err := format.validate(); err != nil {
		return nil, err
	}
	return format, nil
}

func (format *wavFormatChunk) validate() error {
	audioFormat := format.audioFormat
	if audioFormat == WAVE_FORMAT_EXTENSIBLE {
		if format.cbSize != WAV_FMT_EXTENSIBLE_SIZE_BYTES-WAV_FMT_WAVEFORMATEX_SIZE_BYTES {
			return inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", fmt.Errorf("extensible fmt extension size %d is invalid", format.cbSize))
		}
		if format.subFormat != wavPCMSubFormat {
			return inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", errors.New("WAV subformat is not integer PCM"))
		}
		audioFormat = WAVE_FORMAT_PCM
	}
	if audioFormat != WAVE_FORMAT_PCM {
		return inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", fmt.Errorf("WAV audio format 0x%04x is not integer PCM", format.audioFormat))
	}
	if format.channelCount == 0 || format.channelCount > WAV_MAX_CHANNELS {
		return inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", fmt.Errorf("channel count %d is invalid", format.channelCount))
	}
	if format.sampleRateHz < WAV_MIN_SAMPLE_RATE_HZ || format.sampleRateHz > WAV_MAX_SAMPLE_RATE_HZ {
		return inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", fmt.Errorf("sample rate %d is invalid", format.sampleRateHz))
	}
	if format.bitsPerSample%8 != 0 || format.bitsPerSample < WAV_MIN_BIT_DEPTH || format.bitsPerSample > WAV_MAX_BIT_DEPTH {
		return inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", fmt.Errorf("bit depth %d is invalid", format.bitsPerSample))
	}
	bytesPerSample := int(format.bitsPerSample) / 8
	if int(format.blockAlign) != int(format.channelCount)*bytesPerSample {
		return inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", fmt.Errorf("block align %d does not match channels and bit depth", format.blockAlign))
	}
	if format.bytesPerSecond != format.sampleRateHz*uint32(format.blockAlign) {
		return inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("byte rate does not match sample rate and block align"))
	}
	if format.validBits != 0 && format.validBits != format.bitsPerSample {
		return inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", fmt.Errorf("valid bits %d does not match container bit depth %d", format.validBits, format.bitsPerSample))
	}
	return nil
}

func inspectWAVArtwork(picture *id3AttachedPicture) (AlbumArtwork, error) {
	if picture == nil {
		return AlbumArtwork{}, inspectionError(INSPECTION_ERROR_MISSING_ARTWORK, "artwork", errors.New("embedded front cover is required"))
	}
	return validateArtworkData(picture.mimeType, picture.data)
}

// decodePCM verifies the complete data chunk: its length is a whole number of
// frames, every frame is present on disk, and every sample byte is readable.
func (input wavInspectionInput) decodePCM(ctx context.Context, format *wavFormatChunk, data wavDataChunk) (TechnicalAudioProperties, error) {
	if data.size%int64(format.blockAlign) != 0 {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", fmt.Errorf("data chunk size %d is not a whole number of PCM frames", data.size))
	}
	frameCount := data.size / int64(format.blockAlign)
	if frameCount == 0 {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("data chunk contains no PCM frames"))
	}
	if _, err := input.file.Seek(data.offset, io.SeekStart); err != nil {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("seek WAV data chunk: %w", err))
	}
	if err := inspectionCancellationError(ctx); err != nil {
		return TechnicalAudioProperties{}, err
	}
	remaining := data.size
	buffer := make([]byte, 1024*1024)
	for remaining > 0 {
		if err := inspectionCancellationError(ctx); err != nil {
			return TechnicalAudioProperties{}, err
		}
		readSize := int64(len(buffer))
		if remaining < readSize {
			readSize = remaining
		}
		read, readErr := io.ReadFull(input.file, buffer[:readSize])
		remaining -= int64(read)
		readFrames := (data.size - remaining) / int64(format.blockAlign)
		if progressErr := reportDecodedProgress(input.reportProgress, uint64(readFrames), uint64(frameCount), data.size-remaining, data.size, false); progressErr != nil {
			return TechnicalAudioProperties{}, inspectionProgressError(progressErr)
		}
		if readErr != nil {
			return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", fmt.Errorf("read PCM samples: %w", readErr))
		}
	}
	if remaining != 0 {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("data chunk ended early"))
	}
	durationMs := int(uint64(frameCount) * 1000 / uint64(format.sampleRateHz))
	if durationMs <= 0 {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("decoded duration is not positive"))
	}
	bitrateKbps := int((uint64(format.bytesPerSecond)*8 + 500) / 1000)
	if bitrateKbps <= 0 {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("average encoded bitrate is not positive"))
	}
	if progressErr := reportDecodedProgress(input.reportProgress, uint64(frameCount), uint64(frameCount), data.size, data.size, true); progressErr != nil {
		return TechnicalAudioProperties{}, inspectionProgressError(progressErr)
	}
	audio := TechnicalAudioProperties{
		Format:       "wav",
		Container:    "wav",
		Codec:        wavPCMCodec(format.bitsPerSample),
		DurationMs:   durationMs,
		SampleRateHz: int(format.sampleRateHz),
		ChannelCount: int(format.channelCount),
		BitDepth:     int(format.bitsPerSample),
		BitrateKbps:  bitrateKbps,
	}
	return audio, nil
}

func wavPCMCodec(bitDepth uint16) string {
	if bitDepth == 8 {
		return "pcm_u8"
	}
	return fmt.Sprintf("pcm_s%02dle", bitDepth)
}

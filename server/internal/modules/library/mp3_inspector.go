package library

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/hajimehoshi/go-mp3"
)

const (
	ID3_SIGNATURE                            = "ID3"
	ID3V1_SIGNATURE                          = "TAG"
	ID3_SIGNATURE_SIZE_BYTES                 = 3
	ID3_HEADER_SIZE_BYTES                    = 10
	ID3V2_2_FRAME_HEADER_SIZE                = 6
	ID3V2_3_FRAME_HEADER_SIZE                = 10
	ID3V2_2_FRAME_NAME_SIZE                  = 3
	ID3V2_3_FRAME_NAME_SIZE                  = 4
	ID3V2_2_FRAME_SIZE_BYTES                 = 3
	ID3_FRAME_SIZE_BYTES                     = 4
	ID3_FRAME_FLAGS_SIZE_BYTES               = 2
	ID3_SYNC_SAFE_SIZE_BYTES                 = 4
	ID3_SYNC_SAFE_BITS_PER_BYTE              = 7
	ID3_SYNC_SAFE_HIGH_BIT            byte   = 0x80
	ID3_SYNC_SAFE_DATA_MASK           byte   = 0x7f
	ID3_TEXT_ENCODING_OFFSET                 = 0
	ID3_ENCODED_TEXT_OFFSET                  = 1
	ID3_MAJOR_VERSION_OFFSET                 = 3
	ID3_REVISION_OFFSET                      = 4
	ID3_FLAGS_OFFSET                         = 5
	ID3_SIZE_OFFSET                          = 6
	MAX_ID3_TAG_SIZE_BYTES                   = 32 * 1024 * 1024
	MP3_SYNC_BYTE                     byte   = 0xff
	MP3_SYNC_MASK                     byte   = 0xe0
	ID3_FRONT_COVER_TYPE              byte   = 3
	ID3_OTHER_PICTURE_TYPE            byte   = 0
	ID3_VERSION_2                     byte   = 2
	ID3_VERSION_3                     byte   = 3
	ID3_VERSION_4                     byte   = 4
	ID3_TEXT_ENCODING_LATIN1          byte   = 0
	ID3_TEXT_ENCODING_UTF16           byte   = 1
	ID3_TEXT_ENCODING_UTF16BE         byte   = 2
	ID3_TEXT_ENCODING_UTF8            byte   = 3
	ID3V1_TAG_SIZE_BYTES                     = 128
	ID3V1_SIGNATURE_SIZE_BYTES               = 3
	MP3_FRAME_HEADER_SIZE_BYTES              = 4
	MP3_PCM_SAMPLE_SIZE_BYTES                = 4
	MPEG_VERSION_2_5                         = 0
	MPEG_VERSION_RESERVED                    = 1
	MPEG_VERSION_2                           = 2
	MPEG_VERSION_1                           = 3
	MPEG_LAYER_3                             = 1
	MPEG_BITRATE_INDEX_RESERVED              = 15
	MPEG_SAMPLE_RATE_RESERVED                = 3
	MPEG_CHANNEL_MODE_MONO                   = 3
	MPEG_VERSION_SHIFT                       = 19
	MPEG_LAYER_SHIFT                         = 17
	MPEG_BITRATE_SHIFT                       = 12
	MPEG_SAMPLE_RATE_SHIFT                   = 10
	MPEG_PADDING_SHIFT                       = 9
	MPEG_CHANNEL_MODE_SHIFT                  = 6
	MPEG_VERSION_MASK                        = 3
	MPEG_LAYER_MASK                          = 3
	MPEG_BITRATE_MASK                        = 15
	MPEG_SAMPLE_RATE_MASK                    = 3
	MPEG_PADDING_MASK                        = 1
	MPEG_CHANNEL_MODE_MASK                   = 3
	MPEG_FRAME_SYNC_MASK              uint32 = 0xffe00000
	MPEG1_FRAME_COEFFICIENT                  = 144
	MPEG2_FRAME_COEFFICIENT                  = 72
	MPEG1_SAMPLES_PER_FRAME                  = 1152
	MPEG2_SAMPLES_PER_FRAME                  = 576
	BITS_PER_KILOBIT                         = 1000
	BITS_PER_BYTE                            = 8
	MILLISECONDS_PER_SECOND                  = 1000
	MPEG2_SAMPLE_RATE_DIVISOR                = 2
	MPEG2_5_SAMPLE_RATE_DIVISOR              = 4
	MIN_ID3_PICTURE_FRAME_BYTES              = 6
	MIN_ID3_TEXT_FRAME_BYTES                 = 2
	UTF16_CODE_UNIT_BYTES                    = 2
	SINGLE_BYTE_SEPARATOR_SIZE               = 1
	ID3V2_2_PICTURE_FORMAT_SIZE_BYTES        = 3
	UTF16_LITTLE_ENDIAN_BOM_FIRST     byte   = 0xff
	UTF16_LITTLE_ENDIAN_BOM_LAST      byte   = 0xfe
	UTF16_BIG_ENDIAN_BOM_FIRST        byte   = 0xfe
	UTF16_BIG_ENDIAN_BOM_LAST         byte   = 0xff
	MPEG_BITRATE_INDEX_FREE                  = 0
	AUDIO_CHANNEL_COUNT_MONO                 = 1
	AUDIO_CHANNEL_COUNT_STEREO               = 2
	MP3_DECODE_BUFFER_SIZE_BYTES             = 32 * 1024
)

var (
	mp3BaseSampleRates = [...]int{44_100, 48_000, 32_000}
	mp3MPEG1Bitrates   = [...]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
	mp3MPEG2Bitrates   = [...]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
)

type id3Tag struct {
	metadata NormalizedMediaMetadata
	artwork  AlbumArtwork
	offset   int64
}

type id3Frame struct {
	name    string
	payload []byte
}

type id3Values struct {
	tags       map[string][]string
	replayGain map[string]string
	pictures   []taggedID3Picture
}

type id3Picture struct {
	mimeType string
	data     []byte
}

type taggedID3Picture struct {
	picture     id3Picture
	pictureType byte
}

type mp3FrameHeader struct {
	sampleRateHz int
	channelCount int
	frameSize    int
	samples      int
}

type mp3StreamInfo struct {
	sampleRateHz int
	channelCount int
	totalSamples uint64
	encodedBytes int64
}

func inspectOpenMP3(ctx context.Context, file *os.File, reportProgress InspectionProgressReporter) (MediaInspection, error) {
	fileHash, sizeBytes, err := hashAndRewind(ctx, file)
	if err != nil {
		return MediaInspection{}, mp3FileError(err)
	}
	tag, err := inspectID3Tag(file)
	if err != nil {
		return MediaInspection{}, err
	}
	stream, err := inspectMP3Frames(file, tag.offset, sizeBytes)
	if err != nil {
		return MediaInspection{}, err
	}
	audio, err := decodeMP3(ctx, file, stream, reportProgress)
	if err != nil {
		return MediaInspection{}, err
	}
	return MediaInspection{Metadata: tag.metadata, AlbumArtwork: tag.artwork, Audio: audio, FileSHA256: fileHash}, nil
}

func mp3FileError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return inspectionError(INSPECTION_ERROR_VALIDATION_CANCELLED, "validation", err)
	}
	return inspectionError(INSPECTION_ERROR_FILE_READ, "file", err)
}

func inspectID3Tag(file *os.File) (id3Tag, error) {
	frames, version, offset, err := readID3Frames(file)
	if err != nil {
		return id3Tag{}, err
	}
	values, err := collectID3Values(frames, version)
	if err != nil {
		return id3Tag{}, err
	}
	metadata, err := normalizeID3Metadata(values)
	if err != nil {
		return id3Tag{}, err
	}
	picture, err := selectID3FrontCover(values.pictures)
	if err != nil {
		return id3Tag{}, err
	}
	artwork, err := validateArtworkData(picture.mimeType, picture.data)
	if err != nil {
		return id3Tag{}, err
	}
	return id3Tag{metadata: metadata, artwork: artwork, offset: offset}, nil
}

func readID3Frames(file *os.File) ([]id3Frame, byte, int64, error) {
	var header [ID3_HEADER_SIZE_BYTES]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		return nil, 0, 0, invalidID3Error(fmt.Errorf("read ID3 header: %w", err))
	}
	version, size, err := parseID3Header(header[:])
	if err != nil {
		return nil, 0, 0, err
	}
	payload := make([]byte, size)
	if _, err = io.ReadFull(file, payload); err != nil {
		return nil, 0, 0, invalidID3Error(fmt.Errorf("read ID3 payload: %w", err))
	}
	frames, err := parseID3Frames(payload, version)
	return frames, version, int64(ID3_HEADER_SIZE_BYTES + size), err
}

func parseID3Header(header []byte) (byte, int, error) {
	if len(header) < ID3_HEADER_SIZE_BYTES || string(header[:ID3_SIGNATURE_SIZE_BYTES]) != ID3_SIGNATURE {
		return 0, 0, invalidID3Error(errors.New("ID3v2 tag is required"))
	}
	version := header[ID3_MAJOR_VERSION_OFFSET]
	if version < ID3_VERSION_2 || version > ID3_VERSION_4 || header[ID3_REVISION_OFFSET] != 0 {
		return 0, 0, invalidID3Error(fmt.Errorf("unsupported ID3v2 version %d.%d", version, header[ID3_REVISION_OFFSET]))
	}
	if header[ID3_FLAGS_OFFSET] != 0 {
		return 0, 0, invalidID3Error(errors.New("ID3 tag flags are not supported by the Strict Import Profile"))
	}
	size, err := decodeSyncSafeInt(header[ID3_SIZE_OFFSET:ID3_HEADER_SIZE_BYTES])
	if err != nil || size <= 0 || size > MAX_ID3_TAG_SIZE_BYTES {
		return 0, 0, invalidID3Error(errors.New("ID3 tag size is invalid"))
	}
	return version, size, nil
}

func parseID3Frames(payload []byte, version byte) ([]id3Frame, error) {
	var frames []id3Frame
	for offset := 0; offset < len(payload); {
		nameSize, headerSize := ID3V2_3_FRAME_NAME_SIZE, ID3V2_3_FRAME_HEADER_SIZE
		if version == ID3_VERSION_2 {
			nameSize, headerSize = ID3V2_2_FRAME_NAME_SIZE, ID3V2_2_FRAME_HEADER_SIZE
		}
		if len(payload)-offset < headerSize {
			if isZeroPadding(payload[offset:]) {
				break
			}
			return nil, invalidID3Error(errors.New("ID3 frame header is truncated"))
		}
		if isZeroPadding(payload[offset : offset+nameSize]) {
			if !isZeroPadding(payload[offset:]) {
				return nil, invalidID3Error(errors.New("ID3 padding contains non-zero data"))
			}
			break
		}
		frame, size, err := parseID3FrameHeader(payload[offset:], version)
		if err != nil || size <= 0 || size > len(payload)-offset-headerSize {
			return nil, invalidID3Error(errors.New("ID3 frame size is invalid"))
		}
		frame.payload = append([]byte(nil), payload[offset+headerSize:offset+headerSize+size]...)
		frames = append(frames, frame)
		offset += headerSize + size
	}
	return frames, nil
}

func parseID3FrameHeader(data []byte, version byte) (id3Frame, int, error) {
	nameSize := ID3V2_3_FRAME_NAME_SIZE
	if version == ID3_VERSION_2 {
		nameSize = ID3V2_2_FRAME_NAME_SIZE
	}
	name := string(data[:nameSize])
	if !validID3FrameName(name) {
		return id3Frame{}, 0, errors.New("ID3 frame name is invalid")
	}
	if version == ID3_VERSION_2 {
		sizeData := data[nameSize : nameSize+ID3V2_2_FRAME_SIZE_BYTES]
		return id3Frame{name: name}, decodeBigEndianInt(sizeData), nil
	}
	flags := data[ID3V2_3_FRAME_HEADER_SIZE-ID3_FRAME_FLAGS_SIZE_BYTES : ID3V2_3_FRAME_HEADER_SIZE]
	if !isZeroPadding(flags) {
		return id3Frame{}, 0, errors.New("ID3 frame flags are not supported")
	}
	sizeData := data[nameSize : nameSize+ID3_FRAME_SIZE_BYTES]
	if version == ID3_VERSION_4 {
		size, err := decodeSyncSafeInt(sizeData)
		return id3Frame{name: name}, size, err
	}
	return id3Frame{name: name}, int(binary.BigEndian.Uint32(sizeData)), nil
}

func collectID3Values(frames []id3Frame, version byte) (id3Values, error) {
	values := id3Values{tags: make(map[string][]string), replayGain: make(map[string]string)}
	for _, frame := range frames {
		key := canonicalID3FrameName(frame.name, version)
		if key != "" {
			frameValues, err := decodeID3TextValues(frame.payload, version)
			if err != nil {
				return id3Values{}, inspectionError(INSPECTION_ERROR_INVALID_METADATA, key, err)
			}
			if key == "GENRE" {
				frameValues = splitGenreTagValues(frameValues)
			}
			values.tags[key] = append(values.tags[key], frameValues...)
			continue
		}
		if frame.name == id3UserTextFrameName(version) {
			if err := collectID3UserText(values.replayGain, frame.payload); err != nil {
				return id3Values{}, invalidID3Error(err)
			}
			continue
		}
		if frame.name == id3PictureFrameName(version) {
			if err := collectID3Picture(&values, frame.payload, version); err != nil {
				return id3Values{}, err
			}
		}
	}
	return values, nil
}

func normalizeID3Metadata(values id3Values) (NormalizedMediaMetadata, error) {
	replayGain, err := replayGainFromID3(values.replayGain)
	if err != nil {
		return NormalizedMediaMetadata{}, err
	}
	return normalizeMediaMetadata(values.tags, replayGain)
}

func replayGainFromID3(values map[string]string) (ReplayGainMetadata, error) {
	trackGain, err := parseID3ReplayGain(values, "REPLAYGAIN_TRACK_GAIN", parseReplayGainValue)
	if err != nil {
		return ReplayGainMetadata{}, err
	}
	trackPeak, err := parseID3ReplayGain(values, "REPLAYGAIN_TRACK_PEAK", parseReplayGainPeak)
	if err != nil {
		return ReplayGainMetadata{}, err
	}
	albumGain, err := parseID3ReplayGain(values, "REPLAYGAIN_ALBUM_GAIN", parseReplayGainValue)
	if err != nil {
		return ReplayGainMetadata{}, err
	}
	albumPeak, err := parseID3ReplayGain(values, "REPLAYGAIN_ALBUM_PEAK", parseReplayGainPeak)
	if err != nil {
		return ReplayGainMetadata{}, err
	}
	return ReplayGainMetadata{TrackGainDB: trackGain, TrackPeak: trackPeak, AlbumGainDB: albumGain, AlbumPeak: albumPeak}, nil
}

func parseID3ReplayGain(values map[string]string, key string, parse func(string) *float64) (*float64, error) {
	value, exists := values[key]
	if !exists {
		return nil, nil
	}
	parsed := parse(value)
	if parsed == nil {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, key, errors.New("ReplayGain value is invalid"))
	}
	return parsed, nil
}

func canonicalID3FrameName(name string, version byte) string {
	names := map[byte]map[string]string{
		ID3_VERSION_2: {"TT2": "TITLE", "TP1": "ARTIST", "TP2": "ALBUMARTIST", "TAL": "ALBUM", "TRK": "TRACKNUMBER", "TPA": "DISCNUMBER", "TCO": "GENRE", "TYE": "DATE"},
		ID3_VERSION_3: {"TIT2": "TITLE", "TPE1": "ARTIST", "TPE2": "ALBUMARTIST", "TALB": "ALBUM", "TRCK": "TRACKNUMBER", "TPOS": "DISCNUMBER", "TCON": "GENRE", "TYER": "DATE"},
		ID3_VERSION_4: {"TIT2": "TITLE", "TPE1": "ARTIST", "TPE2": "ALBUMARTIST", "TALB": "ALBUM", "TRCK": "TRACKNUMBER", "TPOS": "DISCNUMBER", "TCON": "GENRE", "TDRC": "DATE"},
	}
	return names[version][name]
}

func id3UserTextFrameName(version byte) string {
	if version == ID3_VERSION_2 {
		return "TXX"
	}
	return "TXXX"
}

func id3PictureFrameName(version byte) string {
	if version == ID3_VERSION_2 {
		return "PIC"
	}
	return "APIC"
}

func collectID3UserText(replayGain map[string]string, payload []byte) error {
	description, remainder, err := splitID3EncodedField(payload)
	if err != nil {
		return err
	}
	value, err := decodeID3Text(payload[0], remainder)
	if err != nil {
		return err
	}
	key := strings.ToUpper(strings.TrimSpace(description))
	if strings.HasPrefix(key, "REPLAYGAIN_") {
		if _, exists := replayGain[key]; exists {
			return fmt.Errorf("duplicate %s tag", key)
		}
		replayGain[key] = value
	}
	return nil
}

func collectID3Picture(values *id3Values, payload []byte, version byte) error {
	picture, pictureType, err := decodeID3Picture(payload, version)
	if err != nil {
		return inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", err)
	}
	values.pictures = append(values.pictures, taggedID3Picture{picture: picture, pictureType: pictureType})
	return nil
}

func selectID3FrontCover(pictures []taggedID3Picture) (id3Picture, error) {
	var frontCovers []id3Picture
	for _, picture := range pictures {
		if picture.pictureType == ID3_FRONT_COVER_TYPE {
			frontCovers = append(frontCovers, picture.picture)
		}
	}
	if len(frontCovers) == 1 {
		return frontCovers[0], nil
	}
	if len(frontCovers) > 1 || len(pictures) > 1 {
		return id3Picture{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("embedded front cover role is ambiguous"))
	}
	if len(pictures) == 1 && pictures[0].pictureType == ID3_OTHER_PICTURE_TYPE {
		return pictures[0].picture, nil
	}
	if len(pictures) == 1 {
		return id3Picture{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("embedded picture is not a front cover"))
	}
	return id3Picture{}, inspectionError(INSPECTION_ERROR_MISSING_ARTWORK, "artwork", errors.New("embedded front cover is required"))
}

func decodeID3Picture(payload []byte, version byte) (id3Picture, byte, error) {
	if len(payload) < MIN_ID3_PICTURE_FRAME_BYTES {
		return id3Picture{}, 0, errors.New("embedded picture frame is truncated")
	}
	encoding := payload[ID3_TEXT_ENCODING_OFFSET]
	offset := ID3_ENCODED_TEXT_OFFSET
	mimeType := ""
	if version == ID3_VERSION_2 {
		mimeType = id3v22PictureMIME(string(payload[offset : offset+ID3V2_2_PICTURE_FORMAT_SIZE_BYTES]))
		offset += ID3V2_2_PICTURE_FORMAT_SIZE_BYTES
	} else {
		mimeEnd := bytes.IndexByte(payload[offset:], 0)
		if mimeEnd < 0 {
			return id3Picture{}, 0, errors.New("embedded picture MIME type is truncated")
		}
		mimeType = string(payload[offset : offset+mimeEnd])
		offset += mimeEnd + 1
	}
	if offset >= len(payload) {
		return id3Picture{}, 0, errors.New("embedded picture type is truncated")
	}
	pictureType := payload[offset]
	offset++
	_, imageData, err := splitEncodedField(encoding, payload[offset:])
	if err != nil || len(imageData) == 0 {
		return id3Picture{}, 0, errors.New("embedded picture description or data is truncated")
	}
	return id3Picture{mimeType: mimeType, data: append([]byte(nil), imageData...)}, pictureType, nil
}

func id3v22PictureMIME(format string) string {
	switch strings.ToUpper(format) {
	case "JPG", "JPEG":
		return "image/jpeg"
	case "PNG":
		return "image/png"
	default:
		return ""
	}
}

func decodeID3TextValues(payload []byte, version byte) ([]string, error) {
	if len(payload) < MIN_ID3_TEXT_FRAME_BYTES {
		return nil, errors.New("text frame is empty")
	}
	value, err := decodeID3Text(payload[ID3_TEXT_ENCODING_OFFSET], payload[ID3_ENCODED_TEXT_OFFSET:])
	if err != nil {
		return nil, err
	}
	if version != ID3_VERSION_4 {
		return []string{value}, nil
	}
	return strings.Split(value, "\x00"), nil
}

func splitID3EncodedField(payload []byte) (string, []byte, error) {
	if len(payload) < MIN_ID3_TEXT_FRAME_BYTES {
		return "", nil, errors.New("described text frame is truncated")
	}
	return splitEncodedField(payload[ID3_TEXT_ENCODING_OFFSET], payload[ID3_ENCODED_TEXT_OFFSET:])
}

func splitEncodedField(encoding byte, data []byte) (string, []byte, error) {
	separator := encodedTextSeparator(data, encoding)
	if separator < 0 {
		return "", nil, errors.New("encoded text separator is missing")
	}
	separatorSize := SINGLE_BYTE_SEPARATOR_SIZE
	if encoding == ID3_TEXT_ENCODING_UTF16 || encoding == ID3_TEXT_ENCODING_UTF16BE {
		separatorSize = UTF16_CODE_UNIT_BYTES
	}
	value, err := decodeID3Text(encoding, data[:separator])
	return value, data[separator+separatorSize:], err
}

func encodedTextSeparator(data []byte, encoding byte) int {
	if encoding != ID3_TEXT_ENCODING_UTF16 && encoding != ID3_TEXT_ENCODING_UTF16BE {
		return bytes.IndexByte(data, 0)
	}
	for offset := 0; offset+1 < len(data); offset += UTF16_CODE_UNIT_BYTES {
		if data[offset] == 0 && data[offset+1] == 0 {
			return offset
		}
	}
	return -1
}

func decodeID3Text(encoding byte, data []byte) (string, error) {
	switch encoding {
	case ID3_TEXT_ENCODING_LATIN1:
		runes := make([]rune, len(data))
		for index, value := range data {
			runes[index] = rune(value)
		}
		return string(runes), nil
	case ID3_TEXT_ENCODING_UTF16:
		return decodeID3UTF16(data, true)
	case ID3_TEXT_ENCODING_UTF16BE:
		return decodeID3UTF16(data, false)
	case ID3_TEXT_ENCODING_UTF8:
		if !utf8.Valid(data) {
			return "", errors.New("ID3 text is not valid UTF-8")
		}
		return string(data), nil
	default:
		return "", fmt.Errorf("unsupported ID3 text encoding %d", encoding)
	}
}

func decodeID3UTF16(data []byte, hasBOM bool) (string, error) {
	var order binary.ByteOrder = binary.BigEndian
	if hasBOM {
		if len(data) < UTF16_CODE_UNIT_BYTES {
			return "", errors.New("UTF-16 byte order mark is missing")
		}
		if bytes.Equal(data[:UTF16_CODE_UNIT_BYTES], []byte{UTF16_LITTLE_ENDIAN_BOM_FIRST, UTF16_LITTLE_ENDIAN_BOM_LAST}) {
			order = binary.LittleEndian
		} else if !bytes.Equal(data[:UTF16_CODE_UNIT_BYTES], []byte{UTF16_BIG_ENDIAN_BOM_FIRST, UTF16_BIG_ENDIAN_BOM_LAST}) {
			return "", errors.New("UTF-16 byte order mark is invalid")
		}
		data = data[UTF16_CODE_UNIT_BYTES:]
	}
	if len(data)%UTF16_CODE_UNIT_BYTES != 0 {
		return "", errors.New("UTF-16 text has an odd byte count")
	}
	values := make([]uint16, len(data)/UTF16_CODE_UNIT_BYTES)
	for index := range values {
		offset := index * UTF16_CODE_UNIT_BYTES
		values[index] = order.Uint16(data[offset : offset+UTF16_CODE_UNIT_BYTES])
	}
	return string(utf16.Decode(values)), nil
}

func inspectMP3Frames(file *os.File, audioOffset, sizeBytes int64) (mp3StreamInfo, error) {
	audioEnd, err := mp3AudioEnd(file, sizeBytes)
	if err != nil {
		return mp3StreamInfo{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", err)
	}
	if audioOffset >= audioEnd {
		return mp3StreamInfo{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("MP3 audio stream is empty"))
	}
	if _, err := file.Seek(audioOffset, io.SeekStart); err != nil {
		return mp3StreamInfo{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("seek MP3 audio: %w", err))
	}
	var stream mp3StreamInfo
	for offset := audioOffset; offset < audioEnd; {
		var headerBytes [MP3_FRAME_HEADER_SIZE_BYTES]byte
		if _, err := io.ReadFull(file, headerBytes[:]); err != nil {
			return mp3StreamInfo{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", fmt.Errorf("read MP3 frame header: %w", err))
		}
		header, err := parseMP3FrameHeader(headerBytes[:])
		if err != nil || int64(header.frameSize) > audioEnd-offset {
			return mp3StreamInfo{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("MP3 frame is invalid or truncated"))
		}
		if err := mergeMP3StreamInfo(&stream, header); err != nil {
			return mp3StreamInfo{}, err
		}
		if _, err := file.Seek(int64(header.frameSize-MP3_FRAME_HEADER_SIZE_BYTES), io.SeekCurrent); err != nil {
			return mp3StreamInfo{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("skip MP3 frame: %w", err))
		}
		stream.encodedBytes += int64(header.frameSize)
		offset += int64(header.frameSize)
	}
	return stream, nil
}

func mp3AudioEnd(file *os.File, sizeBytes int64) (int64, error) {
	if sizeBytes < ID3V1_TAG_SIZE_BYTES {
		return sizeBytes, nil
	}
	var signature [ID3V1_SIGNATURE_SIZE_BYTES]byte
	if _, err := file.ReadAt(signature[:], sizeBytes-ID3V1_TAG_SIZE_BYTES); err != nil {
		return 0, fmt.Errorf("read ID3v1 signature: %w", err)
	}
	if string(signature[:ID3V1_SIGNATURE_SIZE_BYTES]) == ID3V1_SIGNATURE {
		return sizeBytes - ID3V1_TAG_SIZE_BYTES, nil
	}
	return sizeBytes, nil
}

func parseMP3FrameHeader(data []byte) (mp3FrameHeader, error) {
	if len(data) < MP3_FRAME_HEADER_SIZE_BYTES {
		return mp3FrameHeader{}, errors.New("MP3 frame header is truncated")
	}
	header := binary.BigEndian.Uint32(data)
	version := int(header>>MPEG_VERSION_SHIFT) & MPEG_VERSION_MASK
	layer := int(header>>MPEG_LAYER_SHIFT) & MPEG_LAYER_MASK
	bitrateIndex := int(header>>MPEG_BITRATE_SHIFT) & MPEG_BITRATE_MASK
	sampleRateIndex := int(header>>MPEG_SAMPLE_RATE_SHIFT) & MPEG_SAMPLE_RATE_MASK
	if header&MPEG_FRAME_SYNC_MASK != MPEG_FRAME_SYNC_MASK || version == MPEG_VERSION_RESERVED || layer != MPEG_LAYER_3 || bitrateIndex == MPEG_BITRATE_INDEX_FREE || bitrateIndex == MPEG_BITRATE_INDEX_RESERVED || sampleRateIndex == MPEG_SAMPLE_RATE_RESERVED {
		return mp3FrameHeader{}, errors.New("unsupported MPEG Layer III frame header")
	}
	sampleRateHz := mp3SampleRate(version, sampleRateIndex)
	bitrateKbps := mp3Bitrate(version, bitrateIndex)
	coefficient, samples := MPEG2_FRAME_COEFFICIENT, MPEG2_SAMPLES_PER_FRAME
	if version == MPEG_VERSION_1 {
		coefficient, samples = MPEG1_FRAME_COEFFICIENT, MPEG1_SAMPLES_PER_FRAME
	}
	frameSize := coefficient*bitrateKbps*BITS_PER_KILOBIT/sampleRateHz + int(header>>MPEG_PADDING_SHIFT&MPEG_PADDING_MASK)
	channelCount := AUDIO_CHANNEL_COUNT_STEREO
	if header>>MPEG_CHANNEL_MODE_SHIFT&MPEG_CHANNEL_MODE_MASK == MPEG_CHANNEL_MODE_MONO {
		channelCount = AUDIO_CHANNEL_COUNT_MONO
	}
	return mp3FrameHeader{sampleRateHz: sampleRateHz, channelCount: channelCount, frameSize: frameSize, samples: samples}, nil
}

func mp3SampleRate(version, index int) int {
	sampleRate := mp3BaseSampleRates[index]
	if version == MPEG_VERSION_2 {
		return sampleRate / MPEG2_SAMPLE_RATE_DIVISOR
	}
	if version == MPEG_VERSION_2_5 {
		return sampleRate / MPEG2_5_SAMPLE_RATE_DIVISOR
	}
	return sampleRate
}

func mp3Bitrate(version, index int) int {
	if version == MPEG_VERSION_1 {
		return mp3MPEG1Bitrates[index]
	}
	return mp3MPEG2Bitrates[index]
}

func mergeMP3StreamInfo(stream *mp3StreamInfo, header mp3FrameHeader) error {
	if stream.totalSamples == 0 {
		stream.sampleRateHz = header.sampleRateHz
		stream.channelCount = header.channelCount
	} else if stream.sampleRateHz != header.sampleRateHz || stream.channelCount != header.channelCount {
		return inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("MP3 stream properties change between frames"))
	}
	stream.totalSamples += uint64(header.samples)
	return nil
}

func decodeMP3(ctx context.Context, file *os.File, stream mp3StreamInfo, reportProgress InspectionProgressReporter) (TechnicalAudioProperties, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("rewind MP3 file: %w", err))
	}
	decoder, err := mp3.NewDecoder(contextReader{ctx: ctx, reader: file})
	if err != nil {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", err)
	}
	decodedSamples, err := decodeMP3ToEOF(ctx, decoder, stream.totalSamples, reportProgress)
	if err != nil {
		return TechnicalAudioProperties{}, err
	}
	if decoder.SampleRate() != stream.sampleRateHz || decodedSamples != stream.totalSamples {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("decoded MP3 sample count or sample rate is inconsistent"))
	}
	durationMs := int(decodedSamples * MILLISECONDS_PER_SECOND / uint64(stream.sampleRateHz))
	bitrateKbps := int((stream.encodedBytes*BITS_PER_BYTE + int64(durationMs)/2) / int64(durationMs))
	if durationMs <= 0 || bitrateKbps <= 0 {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("decoded MP3 technical properties are invalid"))
	}
	return TechnicalAudioProperties{Format: "mp3", Container: "mp3", Codec: "mp3", DurationMs: durationMs, SampleRateHz: stream.sampleRateHz, ChannelCount: stream.channelCount, BitrateKbps: bitrateKbps}, nil
}

func decodeMP3ToEOF(ctx context.Context, decoder *mp3.Decoder, totalSamples uint64, reportProgress InspectionProgressReporter) (uint64, error) {
	buffer := make([]byte, MP3_DECODE_BUFFER_SIZE_BYTES)
	var decodedSamples uint64
	for {
		if err := inspectionCancellationError(ctx); err != nil {
			return 0, err
		}
		read, err := decoder.Read(buffer)
		decodedSamples += uint64(read / MP3_PCM_SAMPLE_SIZE_BYTES)
		if progressErr := reportDecodedProgress(reportProgress, decodedSamples, totalSamples, 0, 0, errors.Is(err, io.EOF)); progressErr != nil {
			return 0, inspectionProgressError(progressErr)
		}
		if errors.Is(err, io.EOF) {
			return decodedSamples, nil
		}
		if err != nil {
			return 0, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", err)
		}
	}
}

func decodeSyncSafeInt(data []byte) (int, error) {
	if len(data) != ID3_SYNC_SAFE_SIZE_BYTES {
		return 0, errors.New("synchsafe integer is invalid")
	}
	value := 0
	for _, part := range data {
		if part&ID3_SYNC_SAFE_HIGH_BIT != 0 {
			return 0, errors.New("synchsafe integer is invalid")
		}
		value = value<<ID3_SYNC_SAFE_BITS_PER_BYTE | int(part&ID3_SYNC_SAFE_DATA_MASK)
	}
	return value, nil
}

func decodeBigEndianInt(data []byte) int {
	value := 0
	for _, part := range data {
		value = value<<BITS_PER_BYTE | int(part)
	}
	return value
}

func validID3FrameName(name string) bool {
	for _, value := range []byte(name) {
		if value < 'A' || value > 'Z' {
			if value < '0' || value > '9' {
				return false
			}
		}
	}
	return true
}

func isZeroPadding(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}

func invalidID3Error(err error) error {
	return inspectionError(INSPECTION_ERROR_INVALID_METADATA, "ID3", err)
}

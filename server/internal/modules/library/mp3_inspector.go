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
	ID3_SIGNATURE                  = "ID3"
	ID3_HEADER_SIZE_BYTES          = 10
	MAX_ID3_TAG_SIZE_BYTES         = 32 * 1024 * 1024
	MP3_SYNC_BYTE             byte = 0xff
	MP3_SYNC_MASK             byte = 0xe0
	ID3_FRONT_COVER_TYPE      byte = 3
	ID3V1_TAG_SIZE_BYTES           = 128
	MP3_PCM_SAMPLE_SIZE_BYTES      = 4
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
	frontCover *id3Picture
}

type id3Picture struct {
	mimeType string
	data     []byte
}

type mp3FrameHeader struct {
	version      int
	sampleRateHz int
	channelCount int
	bitrateKbps  int
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
	if values.frontCover == nil {
		return id3Tag{}, inspectionError(INSPECTION_ERROR_MISSING_ARTWORK, "artwork", errors.New("embedded front cover is required"))
	}
	artwork, err := validateArtworkData(values.frontCover.mimeType, values.frontCover.data)
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
	if string(header[:3]) != ID3_SIGNATURE {
		return nil, 0, 0, invalidID3Error(errors.New("ID3v2 tag is required"))
	}
	version := header[3]
	if version < 2 || version > 4 || header[4] != 0 {
		return nil, 0, 0, invalidID3Error(fmt.Errorf("unsupported ID3v2 version %d.%d", version, header[4]))
	}
	if header[5] != 0 {
		return nil, 0, 0, invalidID3Error(errors.New("ID3 tag flags are not supported by the Strict Import Profile"))
	}
	size, err := decodeSyncSafeInt(header[6:])
	if err != nil || size <= 0 || size > MAX_ID3_TAG_SIZE_BYTES {
		return nil, 0, 0, invalidID3Error(errors.New("ID3 tag size is invalid"))
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(file, payload); err != nil {
		return nil, 0, 0, invalidID3Error(fmt.Errorf("read ID3 payload: %w", err))
	}
	frames, err := parseID3Frames(payload, version)
	return frames, version, int64(ID3_HEADER_SIZE_BYTES + size), err
}

func parseID3Frames(payload []byte, version byte) ([]id3Frame, error) {
	var frames []id3Frame
	for offset := 0; offset < len(payload); {
		nameSize, headerSize := 4, 10
		if version == 2 {
			nameSize, headerSize = 3, 6
		}
		if len(payload)-offset < headerSize {
			if isZeroPadding(payload[offset:]) {
				break
			}
			return nil, invalidID3Error(errors.New("ID3 frame header is truncated"))
		}
		if isZeroPadding(payload[offset : offset+nameSize]) {
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
	nameSize := 4
	if version == 2 {
		nameSize = 3
	}
	name := string(data[:nameSize])
	if !validID3FrameName(name) {
		return id3Frame{}, 0, errors.New("ID3 frame name is invalid")
	}
	if version == 2 {
		return id3Frame{name: name}, int(data[3])<<16 | int(data[4])<<8 | int(data[5]), nil
	}
	if data[8] != 0 || data[9] != 0 {
		return id3Frame{}, 0, errors.New("ID3 frame flags are not supported")
	}
	if version == 4 {
		size, err := decodeSyncSafeInt(data[4:8])
		return id3Frame{name: name}, size, err
	}
	return id3Frame{name: name}, int(binary.BigEndian.Uint32(data[4:8])), nil
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
	names, err := inspectFLACNames(values.tags)
	if err != nil {
		return NormalizedMediaMetadata{}, err
	}
	trackPosition, err := inspectTrackPosition(values.tags)
	if err != nil {
		return NormalizedMediaMetadata{}, err
	}
	discPosition, err := inspectDiscPosition(values.tags)
	if err != nil {
		return NormalizedMediaMetadata{}, err
	}
	year, err := optionalYear(values.tags)
	if err != nil {
		return NormalizedMediaMetadata{}, err
	}
	return NormalizedMediaMetadata{Title: names.Title, Artists: names.Artists, AlbumArtists: names.AlbumArtists, Album: names.Album, TrackPosition: trackPosition, DiscPosition: discPosition, HasDiscNumber: len(values.tags["DISCNUMBER"]) > 0, Genres: names.Genres, Year: year, ReplayGain: replayGainFromID3(values.replayGain)}, nil
}

func replayGainFromID3(values map[string]string) ReplayGainMetadata {
	return ReplayGainMetadata{
		TrackGainDB: parseReplayGainValue(values["REPLAYGAIN_TRACK_GAIN"]),
		TrackPeak:   parseReplayGainPeak(values["REPLAYGAIN_TRACK_PEAK"]),
		AlbumGainDB: parseReplayGainValue(values["REPLAYGAIN_ALBUM_GAIN"]),
		AlbumPeak:   parseReplayGainPeak(values["REPLAYGAIN_ALBUM_PEAK"]),
	}
}

func canonicalID3FrameName(name string, version byte) string {
	names := map[byte]map[string]string{
		2: {"TT2": "TITLE", "TP1": "ARTIST", "TP2": "ALBUMARTIST", "TAL": "ALBUM", "TRK": "TRACKNUMBER", "TPA": "DISCNUMBER", "TCO": "GENRE", "TYE": "DATE"},
		3: {"TIT2": "TITLE", "TPE1": "ARTIST", "TPE2": "ALBUMARTIST", "TALB": "ALBUM", "TRCK": "TRACKNUMBER", "TPOS": "DISCNUMBER", "TCON": "GENRE", "TYER": "DATE"},
		4: {"TIT2": "TITLE", "TPE1": "ARTIST", "TPE2": "ALBUMARTIST", "TALB": "ALBUM", "TRCK": "TRACKNUMBER", "TPOS": "DISCNUMBER", "TCON": "GENRE", "TDRC": "DATE"},
	}
	return names[version][name]
}

func id3UserTextFrameName(version byte) string {
	if version == 2 {
		return "TXX"
	}
	return "TXXX"
}

func id3PictureFrameName(version byte) string {
	if version == 2 {
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
	if pictureType != ID3_FRONT_COVER_TYPE {
		return nil
	}
	if values.frontCover != nil {
		return inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("multiple front covers are ambiguous"))
	}
	values.frontCover = &picture
	return nil
}

func decodeID3Picture(payload []byte, version byte) (id3Picture, byte, error) {
	if len(payload) < 6 {
		return id3Picture{}, 0, errors.New("embedded picture frame is truncated")
	}
	encoding := payload[0]
	offset := 1
	mimeType := ""
	if version == 2 {
		mimeType = id3v22PictureMIME(string(payload[offset : offset+3]))
		offset += 3
	} else {
		mimeEnd := bytes.IndexByte(payload[offset:], 0)
		if mimeEnd < 0 {
			return id3Picture{}, 0, errors.New("embedded picture MIME type is truncated")
		}
		mimeType = string(payload[offset : offset+mimeEnd])
		offset += mimeEnd + 1
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
	if len(payload) < 2 {
		return nil, errors.New("text frame is empty")
	}
	value, err := decodeID3Text(payload[0], payload[1:])
	if err != nil {
		return nil, err
	}
	if version != 4 {
		return []string{value}, nil
	}
	return strings.Split(value, "\x00"), nil
}

func splitID3EncodedField(payload []byte) (string, []byte, error) {
	if len(payload) < 2 {
		return "", nil, errors.New("described text frame is truncated")
	}
	return splitEncodedField(payload[0], payload[1:])
}

func splitEncodedField(encoding byte, data []byte) (string, []byte, error) {
	separator := encodedTextSeparator(data, encoding)
	if separator < 0 {
		return "", nil, errors.New("encoded text separator is missing")
	}
	separatorSize := 1
	if encoding == 1 || encoding == 2 {
		separatorSize = 2
	}
	value, err := decodeID3Text(encoding, data[:separator])
	return value, data[separator+separatorSize:], err
}

func encodedTextSeparator(data []byte, encoding byte) int {
	if encoding != 1 && encoding != 2 {
		return bytes.IndexByte(data, 0)
	}
	for offset := 0; offset+1 < len(data); offset += 2 {
		if data[offset] == 0 && data[offset+1] == 0 {
			return offset
		}
	}
	return -1
}

func decodeID3Text(encoding byte, data []byte) (string, error) {
	switch encoding {
	case 0:
		runes := make([]rune, len(data))
		for index, value := range data {
			runes[index] = rune(value)
		}
		return string(runes), nil
	case 1:
		return decodeID3UTF16(data, true)
	case 2:
		return decodeID3UTF16(data, false)
	case 3:
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
		if len(data) < 2 {
			return "", errors.New("UTF-16 byte order mark is missing")
		}
		if bytes.Equal(data[:2], []byte{0xff, 0xfe}) {
			order = binary.LittleEndian
		} else if !bytes.Equal(data[:2], []byte{0xfe, 0xff}) {
			return "", errors.New("UTF-16 byte order mark is invalid")
		}
		data = data[2:]
	}
	if len(data)%2 != 0 {
		return "", errors.New("UTF-16 text has an odd byte count")
	}
	values := make([]uint16, len(data)/2)
	for index := range values {
		values[index] = order.Uint16(data[index*2 : index*2+2])
	}
	return string(utf16.Decode(values)), nil
}

func inspectMP3Frames(file *os.File, audioOffset, sizeBytes int64) (mp3StreamInfo, error) {
	audioEnd := mp3AudioEnd(file, sizeBytes)
	if audioOffset >= audioEnd {
		return mp3StreamInfo{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("MP3 audio stream is empty"))
	}
	if _, err := file.Seek(audioOffset, io.SeekStart); err != nil {
		return mp3StreamInfo{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("seek MP3 audio: %w", err))
	}
	var stream mp3StreamInfo
	for offset := audioOffset; offset < audioEnd; {
		headerBytes := make([]byte, 4)
		if _, err := io.ReadFull(file, headerBytes); err != nil {
			return mp3StreamInfo{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", fmt.Errorf("read MP3 frame header: %w", err))
		}
		header, err := parseMP3FrameHeader(headerBytes)
		if err != nil || int64(header.frameSize) > audioEnd-offset {
			return mp3StreamInfo{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("MP3 frame is invalid or truncated"))
		}
		if err := mergeMP3StreamInfo(&stream, header); err != nil {
			return mp3StreamInfo{}, err
		}
		if _, err := file.Seek(int64(header.frameSize-4), io.SeekCurrent); err != nil {
			return mp3StreamInfo{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("skip MP3 frame: %w", err))
		}
		stream.encodedBytes += int64(header.frameSize)
		offset += int64(header.frameSize)
	}
	return stream, nil
}

func mp3AudioEnd(file *os.File, sizeBytes int64) int64 {
	if sizeBytes < ID3V1_TAG_SIZE_BYTES {
		return sizeBytes
	}
	var signature [3]byte
	if _, err := file.ReadAt(signature[:], sizeBytes-ID3V1_TAG_SIZE_BYTES); err == nil && string(signature[:]) == "TAG" {
		return sizeBytes - ID3V1_TAG_SIZE_BYTES
	}
	return sizeBytes
}

func parseMP3FrameHeader(data []byte) (mp3FrameHeader, error) {
	if len(data) < 4 {
		return mp3FrameHeader{}, errors.New("MP3 frame header is truncated")
	}
	header := binary.BigEndian.Uint32(data)
	version := int(header>>19) & 3
	layer := int(header>>17) & 3
	bitrateIndex := int(header>>12) & 15
	sampleRateIndex := int(header>>10) & 3
	if header&0xffe00000 != 0xffe00000 || version == 1 || layer != 1 || bitrateIndex == 0 || bitrateIndex == 15 || sampleRateIndex == 3 {
		return mp3FrameHeader{}, errors.New("unsupported MPEG Layer III frame header")
	}
	sampleRateHz := mp3SampleRate(version, sampleRateIndex)
	bitrateKbps := mp3Bitrate(version, bitrateIndex)
	coefficient, samples := 72, 576
	if version == 3 {
		coefficient, samples = 144, 1152
	}
	frameSize := coefficient*bitrateKbps*1000/sampleRateHz + int(header>>9&1)
	channelCount := 2
	if header>>6&3 == 3 {
		channelCount = 1
	}
	return mp3FrameHeader{version: version, sampleRateHz: sampleRateHz, channelCount: channelCount, bitrateKbps: bitrateKbps, frameSize: frameSize, samples: samples}, nil
}

func mp3SampleRate(version, index int) int {
	sampleRate := []int{44_100, 48_000, 32_000}[index]
	if version == 2 {
		return sampleRate / 2
	}
	if version == 0 {
		return sampleRate / 4
	}
	return sampleRate
}

func mp3Bitrate(version, index int) int {
	if version == 3 {
		return []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}[index]
	}
	return []int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}[index]
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
	durationMs := int(decodedSamples * 1000 / uint64(stream.sampleRateHz))
	bitrateKbps := int((stream.encodedBytes*8 + int64(durationMs)/2) / int64(durationMs))
	if durationMs <= 0 || bitrateKbps <= 0 {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("decoded MP3 technical properties are invalid"))
	}
	return TechnicalAudioProperties{Format: "mp3", Container: "mp3", Codec: "mp3", DurationMs: durationMs, SampleRateHz: stream.sampleRateHz, ChannelCount: stream.channelCount, BitrateKbps: bitrateKbps}, nil
}

func decodeMP3ToEOF(ctx context.Context, decoder *mp3.Decoder, totalSamples uint64, reportProgress InspectionProgressReporter) (uint64, error) {
	buffer := make([]byte, 32*1024)
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
	if len(data) != 4 || data[0]&0x80 != 0 || data[1]&0x80 != 0 || data[2]&0x80 != 0 || data[3]&0x80 != 0 {
		return 0, errors.New("synchsafe integer is invalid")
	}
	return int(data[0])<<21 | int(data[1])<<14 | int(data[2])<<7 | int(data[3]), nil
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

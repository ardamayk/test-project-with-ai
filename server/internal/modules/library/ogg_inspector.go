package library

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jfreymuth/oggvorbis"
	flacmeta "github.com/mewkiz/flac/meta"
	"github.com/pion/opus"
	"github.com/pion/opus/pkg/oggreader"
)

const (
	OGG_SIGNATURE              = "OggS"
	VORBIS_IDENTIFICATION      = "\x01vorbis"
	OPUS_IDENTIFICATION        = "OpusHead"
	OPUS_COMMENT_SIGNATURE     = "OpusTags"
	OGG_PAGE_HEADER_SIZE_BYTES = 27
	OGG_END_OF_STREAM_FLAG     = 0x04
	OGG_MAX_SEGMENT_SIZE_BYTES = 255
	OPUS_DECODE_SAMPLE_RATE_HZ = 48000
	OPUS_MAX_FRAME_SAMPLES     = 5760
	PICTURE_FIXED_FIELD_COUNT  = 8
	PICTURE_FIXED_FIELD_SIZE   = 4
	MAX_PICTURE_COMMENT_BYTES  = (MAX_ARTWORK_SIZE_BYTES + 1024) * 2
)

func inspectOpenOGG(ctx context.Context, file *os.File, reportProgress InspectionProgressReporter) (MediaInspection, error) {
	fileHash, sizeBytes, err := hashAndRewind(ctx, file)
	if err != nil {
		return MediaInspection{}, classifyFileReadError(err)
	}
	codec, err := detectOGGCodec(file)
	if err != nil {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", err)
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("rewind OGG stream: %w", err))
	}
	var inspection MediaInspection
	switch codec {
	case "vorbis":
		inspection, err = inspectVorbisOGG(ctx, file, sizeBytes, reportProgress)
	case "opus":
		inspection, err = inspectOpusOGG(ctx, file, sizeBytes, reportProgress)
	}
	if err != nil {
		return MediaInspection{}, err
	}
	inspection.FileSHA256 = fileHash
	return inspection, nil
}

func classifyFileReadError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return inspectionError(INSPECTION_ERROR_VALIDATION_CANCELLED, "validation", err)
	}
	return inspectionError(INSPECTION_ERROR_FILE_READ, "file", err)
}

func detectOGGCodec(file *os.File) (string, error) {
	packet, err := readFirstOGGPacket(file)
	if err != nil {
		return "", err
	}
	switch {
	case bytes.HasPrefix(packet, []byte(VORBIS_IDENTIFICATION)):
		return "vorbis", nil
	case bytes.HasPrefix(packet, []byte(OPUS_IDENTIFICATION)):
		return "opus", nil
	default:
		return "", errors.New("OGG stream codec is unsupported")
	}
}

func readFirstOGGPacket(reader io.Reader) ([]byte, error) {
	header := make([]byte, OGG_PAGE_HEADER_SIZE_BYTES)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, fmt.Errorf("read OGG page header: %w", err)
	}
	if string(header[:len(OGG_SIGNATURE)]) != OGG_SIGNATURE || header[4] != 0 {
		return nil, errors.New("OGG page header is invalid")
	}
	segmentSizes := make([]byte, int(header[26]))
	if _, err := io.ReadFull(reader, segmentSizes); err != nil {
		return nil, fmt.Errorf("read OGG segment table: %w", err)
	}
	packetSize := 0
	for _, size := range segmentSizes {
		packetSize += int(size)
		if size < OGG_MAX_SEGMENT_SIZE_BYTES {
			packet := make([]byte, packetSize)
			if _, err := io.ReadFull(reader, packet); err != nil {
				return nil, fmt.Errorf("read OGG identification packet: %w", err)
			}
			return packet, nil
		}
	}
	return nil, errors.New("OGG identification packet spans pages")
}

func inspectVorbisOGG(ctx context.Context, file *os.File, sizeBytes int64, reportProgress InspectionProgressReporter) (MediaInspection, error) {
	commentHeader, err := oggvorbis.GetCommentHeader(file)
	if err != nil {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "comments", err)
	}
	tags, err := collectCommentTags(commentHeader.Comments)
	if err != nil {
		return MediaInspection{}, err
	}
	metadata, artwork, err := inspectOGGMetadataAndArtwork(tags)
	if err != nil {
		return MediaInspection{}, err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("rewind Vorbis stream: %w", err))
	}
	decoder, err := oggvorbis.NewReader(file)
	if err != nil {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", err)
	}
	audio, err := decodeVorbisToEnd(ctx, file, decoder, sizeBytes, reportProgress)
	if err != nil {
		return MediaInspection{}, err
	}
	return MediaInspection{Metadata: metadata, AlbumArtwork: artwork, Audio: audio}, nil
}

func decodeVorbisToEnd(ctx context.Context, file *os.File, decoder *oggvorbis.Reader, sizeBytes int64, reportProgress InspectionProgressReporter) (TechnicalAudioProperties, error) {
	channels, sampleRate := decoder.Channels(), decoder.SampleRate()
	if channels <= 0 || sampleRate <= 0 {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("vorbis stream has invalid technical properties"))
	}
	buffer := make([]float32, 8192*channels)
	var decodedSamples uint64
	for {
		if err := inspectionCancellationError(ctx); err != nil {
			return TechnicalAudioProperties{}, err
		}
		read, err := decoder.Read(buffer)
		decodedSamples += uint64(read / channels)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", err)
		}
		if read == 0 {
			return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", io.ErrNoProgress)
		}
		if err = reportDecodedProgress(reportProgress, decodedSamples, uint64(decoder.Length()), 0, sizeBytes, false); err != nil {
			return TechnicalAudioProperties{}, inspectionProgressError(err)
		}
	}
	if decoder.Length() <= 0 || decodedSamples != uint64(decoder.Length()) {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("decoded sample count does not match OGG granule position"))
	}
	if err := validateOGGEndOfStream(file); err != nil {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", err)
	}
	durationMs := int(decodedSamples * 1000 / uint64(sampleRate))
	bitrateKbps := vorbisBitrateKbps(decoder, sizeBytes, durationMs)
	if durationMs <= 0 || bitrateKbps <= 0 {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("vorbis duration or bitrate is not positive"))
	}
	if err := reportDecodedProgress(reportProgress, decodedSamples, decodedSamples, sizeBytes, sizeBytes, true); err != nil {
		return TechnicalAudioProperties{}, inspectionProgressError(err)
	}
	return TechnicalAudioProperties{Format: "ogg", Container: "ogg", Codec: "vorbis", DurationMs: durationMs, SampleRateHz: sampleRate, ChannelCount: channels, BitrateKbps: bitrateKbps}, nil
}

func vorbisBitrateKbps(decoder *oggvorbis.Reader, sizeBytes int64, durationMs int) int {
	if decoder.Bitrate().Nominal > 0 {
		return decoder.Bitrate().Nominal / 1000
	}
	return averageBitrateKbps(sizeBytes, durationMs)
}

func inspectOpusOGG(ctx context.Context, file *os.File, sizeBytes int64, reportProgress InspectionProgressReporter) (MediaInspection, error) {
	reader, header, err := oggreader.NewWith(file)
	if err != nil {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", err)
	}
	if header.Version != 1 || header.Channels == 0 || header.Channels > 2 || header.ChannelMap != 0 {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "audio", errors.New("opus stream channel layout is unsupported"))
	}
	commentPacket, _, err := reader.ParseNextPacket()
	if err != nil || !bytes.HasPrefix(commentPacket, []byte(OPUS_COMMENT_SIGNATURE)) {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "comments", errors.New("opus comment header is invalid"))
	}
	comments, err := parseVorbisCommentPayload(commentPacket[len(OPUS_COMMENT_SIGNATURE):])
	if err != nil {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "comments", err)
	}
	tags, err := collectCommentTags(comments)
	if err != nil {
		return MediaInspection{}, err
	}
	metadata, artwork, err := inspectOGGMetadataAndArtwork(tags)
	if err != nil {
		return MediaInspection{}, err
	}
	audio, err := decodeOpusToEnd(ctx, file, reader, header, sizeBytes, reportProgress)
	if err != nil {
		return MediaInspection{}, err
	}
	return MediaInspection{Metadata: metadata, AlbumArtwork: artwork, Audio: audio}, nil
}

func decodeOpusToEnd(ctx context.Context, file *os.File, reader *oggreader.OggReader, header *oggreader.OggHeader, sizeBytes int64, reportProgress InspectionProgressReporter) (TechnicalAudioProperties, error) {
	channels := int(header.Channels)
	decoder, err := opus.NewDecoderWithOutput(OPUS_DECODE_SAMPLE_RATE_HZ, channels)
	if err != nil {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "audio", err)
	}
	output := make([]float32, OPUS_MAX_FRAME_SAMPLES*channels)
	var decodedSamples, encodedBytes, finalGranule uint64
	for {
		if err = inspectionCancellationError(ctx); err != nil {
			return TechnicalAudioProperties{}, err
		}
		packet, pageHeader, packetErr := reader.ParseNextPacket()
		if errors.Is(packetErr, io.EOF) {
			break
		}
		if packetErr != nil {
			return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", packetErr)
		}
		samples, decodeErr := decoder.DecodeToFloat32(packet, output)
		if decodeErr != nil || samples <= 0 {
			return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.Join(decodeErr, errors.New("opus packet decoded no samples")))
		}
		decodedSamples += uint64(samples)
		encodedBytes += uint64(len(packet))
		finalGranule = pageHeader.GranulePosition
		if err = reportDecodedProgress(reportProgress, decodedSamples, 0, int64(encodedBytes), sizeBytes, false); err != nil {
			return TechnicalAudioProperties{}, inspectionProgressError(err)
		}
	}
	if err = validateOGGEndOfStream(file); err != nil {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", err)
	}
	playableSamples, err := playableOpusSamples(decodedSamples, finalGranule, uint64(header.PreSkip))
	if err != nil {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", err)
	}
	durationMs := int(playableSamples * 1000 / OPUS_DECODE_SAMPLE_RATE_HZ)
	bitrateKbps := averageBitrateKbps(int64(encodedBytes), durationMs)
	if durationMs <= 0 || bitrateKbps <= 0 {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("opus duration or bitrate is not positive"))
	}
	if err = reportDecodedProgress(reportProgress, playableSamples, playableSamples, sizeBytes, sizeBytes, true); err != nil {
		return TechnicalAudioProperties{}, inspectionProgressError(err)
	}
	return TechnicalAudioProperties{Format: "opus", Container: "ogg", Codec: "opus", DurationMs: durationMs, SampleRateHz: OPUS_DECODE_SAMPLE_RATE_HZ, ChannelCount: channels, BitrateKbps: bitrateKbps}, nil
}

func playableOpusSamples(decodedSamples, finalGranule, preSkip uint64) (uint64, error) {
	if finalGranule <= preSkip || finalGranule > decodedSamples {
		return 0, errors.New("opus granule position is invalid")
	}
	return finalGranule - preSkip, nil
}

func averageBitrateKbps(encodedBytes int64, durationMs int) int {
	if encodedBytes <= 0 || durationMs <= 0 {
		return 0
	}
	return int((encodedBytes*8 + int64(durationMs)/2) / int64(durationMs))
}

func collectCommentTags(comments []string) (map[string][]string, error) {
	tags := make(map[string][]string)
	for _, comment := range comments {
		key, value, found := strings.Cut(comment, "=")
		key = strings.ToUpper(strings.TrimSpace(key))
		if !found || key == "" {
			return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "comments", errors.New("vorbis comment is malformed"))
		}
		tags[key] = append(tags[key], value)
	}
	return tags, nil
}

func parseVorbisCommentPayload(payload []byte) ([]string, error) {
	vendor, remaining, err := readLengthPrefixedValue(payload)
	if err != nil {
		return nil, err
	}
	_ = vendor
	if len(remaining) < 4 {
		return nil, io.ErrUnexpectedEOF
	}
	count := int(binary.LittleEndian.Uint32(remaining[:4]))
	remaining = remaining[4:]
	if count > len(remaining)/4 {
		return nil, errors.New("vorbis comment count exceeds payload")
	}
	comments := make([]string, 0, count)
	for range count {
		value, next, valueErr := readLengthPrefixedValue(remaining)
		if valueErr != nil {
			return nil, valueErr
		}
		comments = append(comments, string(value))
		remaining = next
	}
	return comments, nil
}

func readLengthPrefixedValue(data []byte) ([]byte, []byte, error) {
	if len(data) < 4 {
		return nil, nil, io.ErrUnexpectedEOF
	}
	length := uint64(binary.LittleEndian.Uint32(data[:4]))
	if length > uint64(len(data)-4) {
		return nil, nil, io.ErrUnexpectedEOF
	}
	end := 4 + int(length)
	return data[4:end], data[end:], nil
}

func inspectOGGMetadataAndArtwork(tags map[string][]string) (NormalizedMediaMetadata, AlbumArtwork, error) {
	metadata, err := inspectVorbisMetadata(tags)
	if err != nil {
		return NormalizedMediaMetadata{}, AlbumArtwork{}, err
	}
	artwork, err := inspectPictureComments(tags["METADATA_BLOCK_PICTURE"])
	if err != nil {
		return NormalizedMediaMetadata{}, AlbumArtwork{}, err
	}
	return metadata, artwork, nil
}

func inspectPictureComments(values []string) (AlbumArtwork, error) {
	var frontCover *flacmeta.Picture
	for _, value := range values {
		picture, err := decodePictureComment(value)
		if err != nil {
			return AlbumArtwork{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", err)
		}
		if picture.Type != FLAC_PICTURE_TYPE_FRONT_COVER {
			continue
		}
		if frontCover != nil {
			return AlbumArtwork{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("multiple front covers are ambiguous"))
		}
		frontCover = picture
	}
	if frontCover == nil {
		return AlbumArtwork{}, inspectionError(INSPECTION_ERROR_MISSING_ARTWORK, "artwork", errors.New("embedded front cover is required"))
	}
	return validateArtwork(frontCover)
}

func decodePictureComment(value string) (*flacmeta.Picture, error) {
	if len(value) == 0 || len(value) > MAX_PICTURE_COMMENT_BYTES {
		return nil, errors.New("encoded picture comment size is invalid")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode picture comment: %w", err)
	}
	reader := bytes.NewReader(data)
	pictureType, err := readPictureUint32(reader)
	if err != nil {
		return nil, err
	}
	mimeType, err := readPictureString(reader)
	if err != nil {
		return nil, err
	}
	if _, err = readPictureString(reader); err != nil {
		return nil, err
	}
	fields := make([]uint32, PICTURE_FIXED_FIELD_COUNT-3)
	for index := range fields {
		if fields[index], err = readPictureUint32(reader); err != nil {
			return nil, err
		}
	}
	if fields[4] > MAX_ARTWORK_SIZE_BYTES || uint64(fields[4]) != uint64(reader.Len()) {
		return nil, errors.New("picture data length is invalid")
	}
	pictureData := make([]byte, int(fields[4]))
	if _, err = io.ReadFull(reader, pictureData); err != nil {
		return nil, err
	}
	return &flacmeta.Picture{Type: pictureType, MIME: mimeType, Width: fields[0], Height: fields[1], Depth: fields[2], NPalColors: fields[3], Data: pictureData}, nil
}

func readPictureString(reader *bytes.Reader) (string, error) {
	length, err := readPictureUint32(reader)
	if err != nil || uint64(length) > uint64(reader.Len()) {
		return "", io.ErrUnexpectedEOF
	}
	data := make([]byte, int(length))
	_, err = io.ReadFull(reader, data)
	return string(data), err
}

func readPictureUint32(reader io.Reader) (uint32, error) {
	var value uint32
	err := binary.Read(reader, binary.BigEndian, &value)
	return value, err
}

func validateOGGEndOfStream(file *os.File) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind OGG stream for EOS validation: %w", err)
	}
	var lastHeaderType byte
	hasPage := false
	for {
		header := make([]byte, OGG_PAGE_HEADER_SIZE_BYTES)
		_, err := io.ReadFull(file, header)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read OGG page header: %w", err)
		}
		if string(header[:len(OGG_SIGNATURE)]) != OGG_SIGNATURE || header[4] != 0 {
			return errors.New("OGG page header is invalid")
		}
		segmentSizes := make([]byte, int(header[26]))
		if _, err = io.ReadFull(file, segmentSizes); err != nil {
			return fmt.Errorf("read OGG segment table: %w", err)
		}
		payloadSize := 0
		for _, size := range segmentSizes {
			payloadSize += int(size)
		}
		if _, err = io.CopyN(io.Discard, file, int64(payloadSize)); err != nil {
			return fmt.Errorf("read OGG page payload: %w", err)
		}
		lastHeaderType = header[5]
		hasPage = true
	}
	if !hasPage || lastHeaderType&OGG_END_OF_STREAM_FLAG == 0 {
		return errors.New("OGG end-of-stream page is missing")
	}
	return nil
}

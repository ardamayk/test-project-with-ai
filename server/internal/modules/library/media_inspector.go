package library

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mewkiz/flac"
	flacmeta "github.com/mewkiz/flac/meta"
	_ "golang.org/x/image/webp"
	"golang.org/x/text/unicode/norm"
)

const (
	MAX_ARTWORK_SIZE_BYTES                       = 20 * 1024 * 1024
	MAX_ARTWORK_PIXELS                           = 50_000_000
	FLAC_SIGNATURE                               = "fLaC"
	FLAC_SIGNATURE_SIZE_BYTES                    = 4
	FLAC_METADATA_BLOCK_HEADER_SIZE_BYTES        = 4
	FLAC_STREAM_INFO_SIZE_BYTES                  = 34
	JPEG_SIGNATURE                               = "\xff\xd8\xff"
	PNG_SIGNATURE                                = "\x89PNG\r\n\x1a\n"
	RIFF_SIGNATURE                               = "RIFF"
	WAVE_SIGNATURE                               = "WAVE"
	WEBP_SIGNATURE                               = "WEBP"
	PNG_ANIMATION_CHUNK                          = "acTL"
	WEBP_EXTENDED_CHUNK                          = "VP8X"
	WEBP_ANIMATION_CHUNK                         = "ANIM"
	WEBP_ANIMATION_FRAME_CHUNK                   = "ANMF"
	PNG_CHUNK_OVERHEAD_BYTES                     = 12
	WEBP_HEADER_SIZE_BYTES                       = 12
	WEBP_CHUNK_HEADER_SIZE_BYTES                 = 8
	RIFF_FORM_TYPE_OFFSET_BYTES                  = 8
	IMAGE_CHUNK_FIELD_SIZE_BYTES                 = 4
	IMAGE_CHUNK_LENGTH_OFFSET_BYTES              = 4
	WEBP_CHUNK_ALIGNMENT_BYTES                   = 2
	WEBP_ANIMATION_FLAG                   byte   = 0x02
	FLAC_PICTURE_TYPE_OTHER               uint32 = 0
	FLAC_PICTURE_TYPE_FRONT_COVER         uint32 = 3
	FLAC_PICTURE_TYPE_BACK_COVER          uint32 = 4
	MAX_IDENTITY_VALUE_BYTES                     = 200
	MAX_MEDIA_POSITION                           = 9999
)

type artworkFormat string

const (
	ARTWORK_FORMAT_JPEG artworkFormat = "jpeg"
	ARTWORK_FORMAT_PNG  artworkFormat = "png"
	ARTWORK_FORMAT_WEBP artworkFormat = "webp"
)

var artworkMIMETypes = map[artworkFormat]string{
	ARTWORK_FORMAT_JPEG: "image/jpeg",
	ARTWORK_FORMAT_PNG:  "image/png",
	ARTWORK_FORMAT_WEBP: "image/webp",
}

var artworkAnimationDetectors = map[artworkFormat]func([]byte) bool{
	ARTWORK_FORMAT_PNG:  hasPNGAnimationChunk,
	ARTWORK_FORMAT_WEBP: hasWebPAnimation,
}

type InspectionErrorCode string

const (
	INSPECTION_ERROR_FILE_READ            InspectionErrorCode = "file_read_failed"
	INSPECTION_ERROR_UNSUPPORTED_FORMAT   InspectionErrorCode = "unsupported_format"
	INSPECTION_ERROR_INVALID_METADATA     InspectionErrorCode = "invalid_metadata"
	INSPECTION_ERROR_MISSING_ARTWORK      InspectionErrorCode = "missing_artwork"
	INSPECTION_ERROR_INVALID_ARTWORK      InspectionErrorCode = "invalid_artwork"
	INSPECTION_ERROR_AUDIO_DECODE         InspectionErrorCode = "audio_decode_failed"
	INSPECTION_ERROR_VALIDATION_CANCELLED InspectionErrorCode = "validation_cancelled"
)

type InspectionError struct {
	Code   InspectionErrorCode
	Field  string
	Reason string
	Err    error
}

func (inspectionErr *InspectionError) Error() string {
	if inspectionErr.Field != "" {
		return fmt.Sprintf("inspect %s: %s: %v", inspectionErr.Field, inspectionErr.Code, inspectionErr.Err)
	}
	return fmt.Sprintf("inspect: %s: %v", inspectionErr.Code, inspectionErr.Err)
}

func (inspectionErr *InspectionError) Unwrap() error {
	return inspectionErr.Err
}

// MediaInspector validates one stable local file through the Strict Import Profile.
// A successful result guarantees that the complete audio stream decoded to EOF.
type MediaInspector interface {
	Inspect(ctx context.Context, path string, reportProgress InspectionProgressReporter) (MediaInspection, error)
}

type InspectionProgress struct {
	DecodedSamples uint64
	TotalSamples   uint64
	Percent        int
}

type InspectionProgressReporter func(InspectionProgress) error

type MediaInspection struct {
	Metadata     NormalizedMediaMetadata
	AlbumArtwork AlbumArtwork
	Audio        TechnicalAudioProperties
	FileSHA256   string
}

type NormalizedMediaMetadata struct {
	Title         string
	Artists       []string
	AlbumArtists  []string
	Album         string
	TrackPosition MediaPosition
	DiscPosition  MediaPosition
	HasDiscNumber bool
	Genres        []string
	Year          int
	ReplayGain    ReplayGainMetadata
}

type MediaPosition struct {
	Number int
	Total  int
}

type AlbumArtwork struct {
	MIMEType string
	Width    int
	Height   int
	Data     []byte
	SHA256   string
}

type TechnicalAudioProperties struct {
	Format       string
	Container    string
	Codec        string
	DurationMs   int
	SampleRateHz int
	ChannelCount int
	BitDepth     int
	BitrateKbps  int
}

type defaultMediaInspector struct{}

func NewMediaInspector() MediaInspector {
	return defaultMediaInspector{}
}

func (defaultMediaInspector) Inspect(ctx context.Context, path string, reportProgress InspectionProgressReporter) (MediaInspection, error) {
	if err := inspectionCancellationError(ctx); err != nil {
		return MediaInspection{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", err)
	}
	isM4A, detectionErr := hasM4ASignature(file)
	if detectionErr != nil {
		closeErr := file.Close()
		if closeErr != nil {
			return MediaInspection{}, errors.Join(detectionErr, inspectionError(INSPECTION_ERROR_FILE_READ, "file", closeErr))
		}
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", detectionErr)
	}
	if isM4A {
		if err := file.Close(); err != nil {
			return MediaInspection{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", err)
		}
		return inspectM4A(ctx, path, reportProgress)
	}
	inspection, inspectionErr := inspectOpenMedia(ctx, file, reportProgress)
	closeErr := file.Close()
	if closeErr != nil {
		closeFailure := inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("close file: %w", closeErr))
		if inspectionErr != nil {
			return MediaInspection{}, errors.Join(inspectionErr, closeFailure)
		}
		return MediaInspection{}, closeFailure
	}
	if inspectionErr != nil {
		return MediaInspection{}, inspectionErr
	}
	return inspection, nil
}

func hasM4ASignature(file *os.File) (bool, error) {
	var header [12]byte
	read, err := io.ReadFull(file, header[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, fmt.Errorf("read media signature: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return false, fmt.Errorf("rewind after media signature: %w", err)
	}
	return read >= 8 && string(header[4:8]) == "ftyp", nil
}

func inspectOpenMedia(ctx context.Context, file *os.File, reportProgress InspectionProgressReporter) (MediaInspection, error) {
	var signature [RIFF_HEADER_SIZE_BYTES]byte
	read, err := io.ReadFull(file, signature[:])
	if err != nil {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", fmt.Errorf("read media signature: %w", err))
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("rewind after media signature: %w", err))
	}
	switch {
	case string(signature[:FLAC_SIGNATURE_SIZE_BYTES]) == FLAC_SIGNATURE:
		return inspectOpenFLAC(ctx, file, reportProgress)
	case string(signature[:len(OGG_SIGNATURE)]) == OGG_SIGNATURE:
		return inspectOpenOGG(ctx, file, reportProgress)
	case string(signature[:len(ID3_SIGNATURE)]) == ID3_SIGNATURE || read >= 2 && signature[0] == MP3_SYNC_BYTE && signature[1]&MP3_SYNC_MASK == MP3_SYNC_MASK:
		return inspectOpenMP3(ctx, file, reportProgress)
	case string(signature[:len(RIFF_SIGNATURE)]) == RIFF_SIGNATURE && string(signature[8:RIFF_HEADER_SIZE_BYTES]) == WAVE_SIGNATURE:
		return inspectOpenWAV(ctx, file, reportProgress)
	}
	return MediaInspection{}, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", errors.New("supported media signature is missing"))
}

func inspectOpenFLAC(ctx context.Context, file *os.File, reportProgress InspectionProgressReporter) (MediaInspection, error) {
	fileHash, sizeBytes, err := hashAndRewind(ctx, file)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return MediaInspection{}, inspectionError(INSPECTION_ERROR_VALIDATION_CANCELLED, "validation", err)
		}
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", err)
	}
	if signatureErr := validateFLACSignature(file); signatureErr != nil {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", signatureErr)
	}
	decoderReader := &countingReader{reader: file}
	stream, err := flac.Parse(decoderReader)
	if err != nil {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", err)
	}

	metadata, err := inspectFLACMetadata(stream.Blocks)
	if err != nil {
		return MediaInspection{}, err
	}
	artwork, err := inspectFLACArtwork(stream.Blocks)
	if err != nil {
		return MediaInspection{}, err
	}
	audio, err := inspectFLACAudio(ctx, stream, decoderReader, sizeBytes, reportProgress)
	if err != nil {
		return MediaInspection{}, err
	}
	return MediaInspection{Metadata: metadata, AlbumArtwork: artwork, Audio: audio, FileSHA256: fileHash}, nil
}

func validateFLACSignature(file *os.File) error {
	var signature [FLAC_SIGNATURE_SIZE_BYTES]byte
	if _, err := io.ReadFull(file, signature[:]); err != nil {
		return fmt.Errorf("read FLAC signature: %w", err)
	}
	if string(signature[:]) != FLAC_SIGNATURE {
		return errors.New("FLAC signature is missing")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind after FLAC signature: %w", err)
	}
	return nil
}

func hashAndRewind(ctx context.Context, file *os.File) (string, int64, error) {
	fileHash := sha256.New()
	sizeBytes, err := io.Copy(fileHash, contextReader{ctx: ctx, reader: file})
	if err != nil {
		return "", 0, fmt.Errorf("hash file: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", 0, fmt.Errorf("rewind file: %w", err)
	}
	return hex.EncodeToString(fileHash.Sum(nil)), sizeBytes, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

type countingReader struct {
	reader    io.Reader
	bytesRead int64
}

func (reader *countingReader) Read(buffer []byte) (int, error) {
	read, err := reader.reader.Read(buffer)
	reader.bytesRead += int64(read)
	return read, err
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

func inspectFLACMetadata(blocks []*flacmeta.Block) (NormalizedMediaMetadata, error) {
	tags := collectVorbisTags(blocks)
	return normalizeMediaMetadata(tags, replayGainFromTags(tags))
}

func normalizeMediaMetadata(tags map[string][]string, replayGain ReplayGainMetadata) (NormalizedMediaMetadata, error) {
	names, err := inspectVorbisNames(tags)
	if err != nil {
		return NormalizedMediaMetadata{}, err
	}
	trackPosition, err := inspectTrackPosition(tags)
	if err != nil {
		return NormalizedMediaMetadata{}, err
	}
	discPosition, err := inspectDiscPosition(tags)
	if err != nil {
		return NormalizedMediaMetadata{}, err
	}
	year, err := optionalYear(tags)
	if err != nil {
		return NormalizedMediaMetadata{}, err
	}
	return NormalizedMediaMetadata{
		Title:         names.Title,
		Artists:       names.Artists,
		AlbumArtists:  names.AlbumArtists,
		Album:         names.Album,
		TrackPosition: trackPosition,
		DiscPosition:  discPosition,
		HasDiscNumber: len(tags["DISCNUMBER"]) > 0,
		Genres:        names.Genres,
		Year:          year,
		ReplayGain:    replayGain,
	}, nil
}

func replayGainFromTags(tags map[string][]string) ReplayGainMetadata {
	values := make(map[string]string, len(tags))
	for key, tagValues := range tags {
		if len(tagValues) > 0 {
			values[key] = tagValues[0]
		}
	}
	return readReplayGainStringMetadata(values)
}

func inspectTrackPosition(tags map[string][]string) (MediaPosition, error) {
	position, err := requiredPosition(tags, "TRACKNUMBER")
	if err != nil {
		return MediaPosition{}, err
	}
	return mergePositionTotal(tags, position, "TOTALTRACKS")
}

func inspectDiscPosition(tags map[string][]string) (MediaPosition, error) {
	hasDiscNumber := len(tags["DISCNUMBER"]) > 0
	position := MediaPosition{Number: 1}
	var err error
	if hasDiscNumber {
		position, err = requiredPosition(tags, "DISCNUMBER")
		if err != nil {
			return MediaPosition{}, err
		}
	}
	position, err = mergePositionTotal(tags, position, "TOTALDISCS")
	if err != nil {
		return MediaPosition{}, err
	}
	if !hasDiscNumber && position.Total > 1 {
		return MediaPosition{}, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "DISCNUMBER", errors.New("required for a multi-disc Album"))
	}
	return position, nil
}

type normalizedMediaNames struct {
	Title        string
	Artists      []string
	AlbumArtists []string
	Album        string
	Genres       []string
}

func inspectVorbisNames(tags map[string][]string) (normalizedMediaNames, error) {
	var names normalizedMediaNames
	var err error
	if names.Title, err = requiredSingleTag(tags, "TITLE"); err != nil {
		return normalizedMediaNames{}, err
	}
	if names.Artists, err = requiredTags(tags, "ARTIST"); err != nil {
		return normalizedMediaNames{}, err
	}
	if names.AlbumArtists, err = requiredTags(tags, "ALBUMARTIST"); err != nil {
		return normalizedMediaNames{}, err
	}
	if names.Album, err = requiredSingleTag(tags, "ALBUM"); err != nil {
		return normalizedMediaNames{}, err
	}
	if names.Genres, err = requiredTags(tags, "GENRE"); err != nil {
		return normalizedMediaNames{}, err
	}
	return names, nil
}

func collectVorbisTags(blocks []*flacmeta.Block) map[string][]string {
	tags := make(map[string][]string)
	for _, block := range blocks {
		comment, ok := block.Body.(*flacmeta.VorbisComment)
		if !ok {
			continue
		}
		for _, tag := range comment.Tags {
			key := strings.ToUpper(strings.TrimSpace(tag[0]))
			tags[key] = append(tags[key], tag[1])
		}
	}
	return tags
}

func requiredSingleTag(tags map[string][]string, key string) (string, error) {
	values, err := requiredTags(tags, key)
	if err != nil {
		return "", err
	}
	if len(values) != 1 {
		return "", inspectionError(INSPECTION_ERROR_INVALID_METADATA, key, errors.New("expected exactly one value"))
	}
	return values[0], nil
}

func requiredTags(tags map[string][]string, key string) ([]string, error) {
	rawValues := tags[key]
	if len(rawValues) == 0 {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, key, errors.New("required tag is missing"))
	}
	values := make([]string, 0, len(rawValues))
	for _, rawValue := range rawValues {
		value, isValid := normalizeMetadataValue(rawValue)
		if !isValid {
			return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, key, errors.New("tag is empty, too long, or contains unsafe characters"))
		}
		values = append(values, value)
	}
	return values, nil
}

func normalizeMetadataValue(value string) (string, bool) {
	if !utf8.ValidString(value) || strings.IndexFunc(value, isUnsafeIdentityRune) >= 0 {
		return "", false
	}
	value = norm.NFC.String(strings.TrimSpace(value))
	if len(value) > MAX_IDENTITY_VALUE_BYTES || strings.IndexFunc(value, isUnsafeIdentityRune) >= 0 {
		return "", false
	}
	value = strings.Join(strings.Fields(value), " ")
	return value, value != ""
}

func isUnsafeIdentityRune(value rune) bool {
	return unicode.IsControl(value) ||
		unicode.Is(unicode.Bidi_Control, value) ||
		unicode.Is(unicode.Noncharacter_Code_Point, value)
}

func requiredPosition(tags map[string][]string, key string) (MediaPosition, error) {
	value, err := requiredSingleTag(tags, key)
	if err != nil {
		return MediaPosition{}, err
	}
	position, err := parsePosition(value)
	if err != nil {
		return MediaPosition{}, inspectionError(INSPECTION_ERROR_INVALID_METADATA, key, fmt.Errorf("position must be between 1 and %d with an optional total: %w", MAX_MEDIA_POSITION, err))
	}
	return position, nil
}

func parsePosition(value string) (MediaPosition, error) {
	parts := strings.Split(value, "/")
	if len(parts) > 2 {
		return MediaPosition{}, errors.New("too many position parts")
	}
	position, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return MediaPosition{}, err
	}
	if position <= 0 || position > MAX_MEDIA_POSITION {
		return MediaPosition{}, errors.New("position is outside the supported range")
	}
	if len(parts) == 1 || strings.TrimSpace(parts[1]) == "" {
		return MediaPosition{Number: position}, nil
	}
	total, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || total < position || total > MAX_MEDIA_POSITION {
		return MediaPosition{}, errors.New("position total is invalid")
	}
	return MediaPosition{Number: position, Total: total}, nil
}

func mergePositionTotal(tags map[string][]string, position MediaPosition, totalKey string) (MediaPosition, error) {
	total, hasTotal, err := optionalTotal(tags, totalKey)
	if err != nil {
		return MediaPosition{}, err
	}
	if !hasTotal {
		return position, nil
	}
	if position.Total > 0 && position.Total != total {
		return MediaPosition{}, inspectionError(INSPECTION_ERROR_INVALID_METADATA, totalKey, errors.New("total conflicts with the position tag"))
	}
	if total < position.Number {
		return MediaPosition{}, inspectionError(INSPECTION_ERROR_INVALID_METADATA, totalKey, errors.New("total must not be less than the position"))
	}
	position.Total = total
	return position, nil
}

func optionalTotal(tags map[string][]string, key string) (int, bool, error) {
	if len(tags[key]) == 0 {
		return 0, false, nil
	}
	value, err := requiredSingleTag(tags, key)
	if err != nil {
		return 0, false, err
	}
	total, parseErr := strconv.Atoi(value)
	if parseErr != nil || total <= 0 || total > MAX_MEDIA_POSITION {
		return 0, false, inspectionError(INSPECTION_ERROR_INVALID_METADATA, key, fmt.Errorf("total must be between 1 and %d", MAX_MEDIA_POSITION))
	}
	return total, true, nil
}

func optionalYear(tags map[string][]string) (int, error) {
	if len(tags["DATE"]) == 0 {
		return 0, nil
	}
	value, err := requiredSingleTag(tags, "DATE")
	if err != nil {
		return 0, err
	}
	if len(value) < 4 {
		return 0, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "DATE", errors.New("date must begin with a four-digit year"))
	}
	year, parseErr := strconv.Atoi(value[:4])
	if parseErr != nil || year <= 0 {
		return 0, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "DATE", errors.New("date must begin with a four-digit year"))
	}
	return year, nil
}

func inspectFLACArtwork(blocks []*flacmeta.Block) (AlbumArtwork, error) {
	var frontCover *flacmeta.Picture
	for _, block := range blocks {
		picture, ok := block.Body.(*flacmeta.Picture)
		if !ok {
			continue
		}
		if picture.Type == FLAC_PICTURE_TYPE_FRONT_COVER {
			if frontCover != nil {
				return AlbumArtwork{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("multiple front covers are ambiguous"))
			}
			frontCover = picture
		}
	}
	if frontCover != nil {
		return validateArtwork(frontCover)
	}
	return AlbumArtwork{}, inspectionError(INSPECTION_ERROR_MISSING_ARTWORK, "artwork", errors.New("embedded front cover is required"))
}

func validateArtwork(picture *flacmeta.Picture) (AlbumArtwork, error) {
	return validateArtworkData(picture.MIME, picture.Data)
}

func validateArtworkData(mimeType string, data []byte) (AlbumArtwork, error) {
	format, err := validateArtworkFormat(mimeType, data)
	if err != nil {
		return AlbumArtwork{}, err
	}
	config, err := decodeArtwork(data, format)
	if err != nil {
		return AlbumArtwork{}, err
	}
	hash := sha256.Sum256(data)
	return AlbumArtwork{MIMEType: artworkMIMETypes[format], Width: config.Width, Height: config.Height, Data: append([]byte(nil), data...), SHA256: hex.EncodeToString(hash[:])}, nil
}

func validateArtworkFormat(mimeType string, data []byte) (artworkFormat, error) {
	if len(data) == 0 || len(data) > MAX_ARTWORK_SIZE_BYTES {
		return "", inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("encoded artwork size is invalid"))
	}
	format := detectArtworkFormat(data)
	if artworkMIMETypes[format] == "" || mimeType != artworkMIMETypes[format] {
		return "", inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("declared and detected image formats differ or are unsupported"))
	}
	if isAnimatedArtwork(format, data) {
		return "", inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("animated artwork is not supported"))
	}
	return format, nil
}

func decodeArtwork(data []byte, format artworkFormat) (image.Config, error) {
	config, decodedFormat, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return image.Config{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", fmt.Errorf("decode image config: %w", err))
	}
	if artworkFormat(decodedFormat) != format {
		return image.Config{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("declared and detected image formats differ or are unsupported"))
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > MAX_ARTWORK_PIXELS {
		return image.Config{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("decoded artwork dimensions are invalid"))
	}
	if _, decodedFormat, err = image.Decode(bytes.NewReader(data)); err != nil {
		return image.Config{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", fmt.Errorf("decode image: %w", err))
	}
	if artworkFormat(decodedFormat) != format {
		return image.Config{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("decoded image format changed"))
	}
	return config, nil
}

func detectArtworkFormat(data []byte) artworkFormat {
	switch {
	case len(data) >= len(JPEG_SIGNATURE) && string(data[:len(JPEG_SIGNATURE)]) == JPEG_SIGNATURE:
		return ARTWORK_FORMAT_JPEG
	case len(data) >= len(PNG_SIGNATURE) && string(data[:len(PNG_SIGNATURE)]) == PNG_SIGNATURE:
		return ARTWORK_FORMAT_PNG
	case len(data) >= WEBP_HEADER_SIZE_BYTES && string(data[:len(RIFF_SIGNATURE)]) == RIFF_SIGNATURE && string(data[RIFF_FORM_TYPE_OFFSET_BYTES:WEBP_HEADER_SIZE_BYTES]) == WEBP_SIGNATURE:
		return ARTWORK_FORMAT_WEBP
	default:
		return ""
	}
}

func isAnimatedArtwork(format artworkFormat, data []byte) bool {
	detector := artworkAnimationDetectors[format]
	if detector == nil {
		return false
	}
	return detector(data)
}

func hasPNGAnimationChunk(data []byte) bool {
	for offset := len(PNG_SIGNATURE); offset+PNG_CHUNK_OVERHEAD_BYTES <= len(data); {
		length := int(binary.BigEndian.Uint32(data[offset : offset+IMAGE_CHUNK_FIELD_SIZE_BYTES]))
		if length > len(data)-offset-PNG_CHUNK_OVERHEAD_BYTES {
			return false
		}
		chunkTypeOffset := offset + IMAGE_CHUNK_LENGTH_OFFSET_BYTES
		if string(data[chunkTypeOffset:chunkTypeOffset+IMAGE_CHUNK_FIELD_SIZE_BYTES]) == PNG_ANIMATION_CHUNK {
			return true
		}
		offset += PNG_CHUNK_OVERHEAD_BYTES + length
	}
	return false
}

func hasWebPAnimation(data []byte) bool {
	for offset := WEBP_HEADER_SIZE_BYTES; offset+WEBP_CHUNK_HEADER_SIZE_BYTES <= len(data); {
		lengthOffset := offset + IMAGE_CHUNK_LENGTH_OFFSET_BYTES
		length := int(binary.LittleEndian.Uint32(data[lengthOffset : lengthOffset+IMAGE_CHUNK_FIELD_SIZE_BYTES]))
		dataOffset := offset + WEBP_CHUNK_HEADER_SIZE_BYTES
		if length > len(data)-dataOffset {
			return false
		}
		chunkType := string(data[offset : offset+IMAGE_CHUNK_FIELD_SIZE_BYTES])
		if chunkType == WEBP_ANIMATION_CHUNK || chunkType == WEBP_ANIMATION_FRAME_CHUNK || chunkType == WEBP_EXTENDED_CHUNK && length > 0 && data[dataOffset]&WEBP_ANIMATION_FLAG != 0 {
			return true
		}
		offset = dataOffset + length + length%WEBP_CHUNK_ALIGNMENT_BYTES
	}
	return false
}

func inspectFLACAudio(ctx context.Context, stream *flac.Stream, decoderReader *countingReader, sizeBytes int64, reportProgress InspectionProgressReporter) (TechnicalAudioProperties, error) {
	decodedHash := md5.New()
	var decodedSamples uint64
	for {
		if err := inspectionCancellationError(ctx); err != nil {
			return TechnicalAudioProperties{}, err
		}
		frame, err := stream.ParseNext()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", err)
		}
		if len(frame.Subframes) == 0 || len(frame.Subframes[0].Samples) == 0 {
			return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("decoded frame contains no samples"))
		}
		decodedSamples += uint64(len(frame.Subframes[0].Samples))
		frame.Hash(decodedHash)
		if err := reportDecodedProgress(reportProgress, decodedSamples, stream.Info.NSamples, decoderReader.bytesRead, sizeBytes, false); err != nil {
			return TechnicalAudioProperties{}, inspectionProgressError(err)
		}
	}
	audio, err := buildFLACAudioProperties(stream.Info, stream.Blocks, decodedHash.Sum(nil), decodedSamples, sizeBytes)
	if err != nil {
		return TechnicalAudioProperties{}, err
	}
	if err := reportDecodedProgress(reportProgress, decodedSamples, stream.Info.NSamples, decoderReader.bytesRead, sizeBytes, true); err != nil {
		return TechnicalAudioProperties{}, inspectionProgressError(err)
	}
	return audio, nil
}

func buildFLACAudioProperties(info *flacmeta.StreamInfo, blocks []*flacmeta.Block, decodedHash []byte, decodedSamples uint64, sizeBytes int64) (TechnicalAudioProperties, error) {
	if decodedSamples == 0 || info.SampleRate == 0 || info.NChannels == 0 || info.BitsPerSample == 0 {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("decoded stream has invalid technical properties"))
	}
	if info.NSamples != 0 && decodedSamples != info.NSamples {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("decoded sample count does not match STREAMINFO"))
	}
	var emptyMD5 [md5.Size]byte
	if info.MD5sum != emptyMD5 && !bytes.Equal(decodedHash, info.MD5sum[:]) {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("decoded audio MD5 does not match STREAMINFO"))
	}
	durationMs := int(decodedSamples * 1000 / uint64(info.SampleRate))
	if durationMs <= 0 {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("decoded duration is not positive"))
	}
	audioSizeBytes := encodedFLACAudioSize(sizeBytes, blocks)
	if audioSizeBytes <= 0 {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("encoded audio size is not positive"))
	}
	bitrateKbps := int((audioSizeBytes*8 + int64(durationMs)/2) / int64(durationMs))
	if bitrateKbps <= 0 {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("average encoded bitrate is not positive"))
	}
	return TechnicalAudioProperties{Format: "flac", Container: "flac", Codec: "flac", DurationMs: durationMs, SampleRateHz: int(info.SampleRate), ChannelCount: int(info.NChannels), BitDepth: int(info.BitsPerSample), BitrateKbps: bitrateKbps}, nil
}

func inspectionCancellationError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return inspectionError(INSPECTION_ERROR_VALIDATION_CANCELLED, "validation", err)
	}
	return nil
}

func inspectionProgressError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return inspectionError(INSPECTION_ERROR_VALIDATION_CANCELLED, "validation", err)
	}
	return err
}

func reportDecodedProgress(reportProgress InspectionProgressReporter, decodedSamples, totalSamples uint64, encodedBytes, totalBytes int64, isComplete bool) error {
	if reportProgress == nil {
		return nil
	}
	percent := 0
	if totalSamples > 0 {
		percent = int(decodedSamples * 100 / totalSamples)
	} else if totalBytes > 0 {
		percent = int(encodedBytes * 100 / totalBytes)
	}
	if isComplete {
		percent = 100
	} else if percent >= 100 {
		percent = 99
	}
	return reportProgress(InspectionProgress{DecodedSamples: decodedSamples, TotalSamples: totalSamples, Percent: percent})
}

func encodedFLACAudioSize(sizeBytes int64, blocks []*flacmeta.Block) int64 {
	metadataSizeBytes := int64(FLAC_SIGNATURE_SIZE_BYTES + FLAC_METADATA_BLOCK_HEADER_SIZE_BYTES + FLAC_STREAM_INFO_SIZE_BYTES)
	for _, block := range blocks {
		metadataSizeBytes += int64(FLAC_METADATA_BLOCK_HEADER_SIZE_BYTES) + block.Length
	}
	return sizeBytes - metadataSizeBytes
}

func inspectionError(code InspectionErrorCode, field string, err error) *InspectionError {
	return &InspectionError{Code: code, Field: field, Reason: publicInspectionReason(code, err), Err: err}
}

func publicInspectionReason(code InspectionErrorCode, err error) string {
	switch code {
	case INSPECTION_ERROR_INVALID_METADATA:
		reason := err.Error()
		if separatorIndex := strings.Index(reason, ": "); separatorIndex >= 0 {
			return reason[:separatorIndex]
		}
		return reason
	case INSPECTION_ERROR_MISSING_ARTWORK:
		return err.Error()
	case INSPECTION_ERROR_FILE_READ:
		return "file could not be read"
	case INSPECTION_ERROR_UNSUPPORTED_FORMAT:
		return "file container or audio codec is not supported"
	case INSPECTION_ERROR_INVALID_ARTWORK:
		return "embedded artwork is invalid"
	case INSPECTION_ERROR_AUDIO_DECODE:
		return "audio stream failed full decode"
	default:
		return "validation failed"
	}
}

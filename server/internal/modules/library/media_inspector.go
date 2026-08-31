package library

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
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
	"golang.org/x/text/unicode/norm"
)

const (
	MAX_ARTWORK_SIZE_BYTES                = 20 * 1024 * 1024
	MAX_ARTWORK_PIXELS                    = 50_000_000
	FLAC_SIGNATURE_SIZE_BYTES             = 4
	FLAC_METADATA_BLOCK_HEADER_SIZE_BYTES = 4
	FLAC_STREAM_INFO_SIZE_BYTES           = 34
	MAX_IDENTITY_VALUE_BYTES              = 200
	MAX_MEDIA_POSITION                    = 9999
)

type InspectionErrorCode string

const (
	INSPECTION_ERROR_FILE_READ          InspectionErrorCode = "file_read_failed"
	INSPECTION_ERROR_UNSUPPORTED_FORMAT InspectionErrorCode = "unsupported_format"
	INSPECTION_ERROR_INVALID_METADATA   InspectionErrorCode = "invalid_metadata"
	INSPECTION_ERROR_MISSING_ARTWORK    InspectionErrorCode = "missing_artwork"
	INSPECTION_ERROR_INVALID_ARTWORK    InspectionErrorCode = "invalid_artwork"
	INSPECTION_ERROR_AUDIO_DECODE       InspectionErrorCode = "audio_decode_failed"
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
	Inspect(path string) (MediaInspection, error)
}

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

func (defaultMediaInspector) Inspect(path string) (MediaInspection, error) {
	file, err := os.Open(path)
	if err != nil {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", err)
	}
	inspection, inspectionErr := inspectOpenFLAC(file)
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

func inspectOpenFLAC(file *os.File) (MediaInspection, error) {
	fileHash, sizeBytes, err := hashAndRewind(file)
	if err != nil {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_FILE_READ, "file", err)
	}
	if signatureErr := validateFLACSignature(file); signatureErr != nil {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "format", signatureErr)
	}
	stream, err := flac.Parse(file)
	if err != nil {
		return MediaInspection{}, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "format", err)
	}

	metadata, err := inspectFLACMetadata(stream.Blocks)
	if err != nil {
		return MediaInspection{}, err
	}
	artwork, err := inspectFLACArtwork(stream.Blocks)
	if err != nil {
		return MediaInspection{}, err
	}
	audio, err := inspectFLACAudio(stream, sizeBytes)
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
	if string(signature[:]) != "fLaC" {
		return errors.New("FLAC signature is missing")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind after FLAC signature: %w", err)
	}
	return nil
}

func hashAndRewind(file *os.File) (string, int64, error) {
	fileHash := sha256.New()
	sizeBytes, err := io.Copy(fileHash, file)
	if err != nil {
		return "", 0, fmt.Errorf("hash file: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", 0, fmt.Errorf("rewind file: %w", err)
	}
	return hex.EncodeToString(fileHash.Sum(nil)), sizeBytes, nil
}

func inspectFLACMetadata(blocks []*flacmeta.Block) (NormalizedMediaMetadata, error) {
	tags := collectVorbisTags(blocks)
	names, err := inspectFLACNames(tags)
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
	}, nil
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

func inspectFLACNames(tags map[string][]string) (normalizedMediaNames, error) {
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
		if !ok || picture.Type != 3 {
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

func validateArtwork(picture *flacmeta.Picture) (AlbumArtwork, error) {
	if len(picture.Data) == 0 || len(picture.Data) > MAX_ARTWORK_SIZE_BYTES {
		return AlbumArtwork{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("encoded artwork size is invalid"))
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(picture.Data))
	if err != nil {
		return AlbumArtwork{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", fmt.Errorf("decode image: %w", err))
	}
	mimeType := map[string]string{"jpeg": "image/jpeg", "png": "image/png"}[format]
	if mimeType == "" || picture.MIME != mimeType {
		return AlbumArtwork{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("declared and detected image formats differ or are unsupported"))
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > MAX_ARTWORK_PIXELS {
		return AlbumArtwork{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("decoded artwork dimensions are invalid"))
	}
	hash := sha256.Sum256(picture.Data)
	return AlbumArtwork{MIMEType: mimeType, Width: config.Width, Height: config.Height, Data: append([]byte(nil), picture.Data...), SHA256: hex.EncodeToString(hash[:])}, nil
}

func inspectFLACAudio(stream *flac.Stream, sizeBytes int64) (TechnicalAudioProperties, error) {
	decodedHash := md5.New()
	var decodedSamples uint64
	for {
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
	}
	return buildFLACAudioProperties(stream.Info, stream.Blocks, decodedHash.Sum(nil), decodedSamples, sizeBytes)
}

func buildFLACAudioProperties(info *flacmeta.StreamInfo, blocks []*flacmeta.Block, decodedHash []byte, decodedSamples uint64, sizeBytes int64) (TechnicalAudioProperties, error) {
	if decodedSamples == 0 || info.SampleRate == 0 || info.NChannels == 0 {
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
	return TechnicalAudioProperties{Format: "flac", Codec: "flac", DurationMs: durationMs, SampleRateHz: int(info.SampleRate), ChannelCount: int(info.NChannels), BitDepth: int(info.BitsPerSample), BitrateKbps: bitrateKbps}, nil
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
		return "file is not a supported FLAC stream"
	case INSPECTION_ERROR_INVALID_ARTWORK:
		return "embedded artwork is invalid"
	case INSPECTION_ERROR_AUDIO_DECODE:
		return "audio stream failed full decode"
	default:
		return "validation failed"
	}
}

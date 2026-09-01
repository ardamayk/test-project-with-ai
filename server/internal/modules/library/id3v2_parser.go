package library

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"unicode/utf16"
	"unicode/utf8"
)

// Minimal strict ID3v2.3/2.4 reader used by the WAV inspector. It only accepts
// frames the Strict Import Profile understands and rejects everything else.

const (
	ID3V2_FILE_SIGNATURE               = "ID3"
	ID3V2_HEADER_SIZE_BYTES            = 10
	ID3V2_FOOTER_SIZE_BYTES            = 10
	ID3V2_UNSYNCHRONISATION_FLAG  byte = 0x80
	ID3V2_EXTENDED_HEADER_FLAG    byte = 0x40
	ID3V2_EXPERIMENTAL_FLAG       byte = 0x20
	ID3V2_FOOTER_FLAG             byte = 0x10
	ID3V2_MAJOR_VERSION_MIN            = 3
	ID3V2_MAJOR_VERSION_MAX            = 4
	ID3V2_MAX_TAG_BYTES                = 16 * 1024 * 1024
	ID3V2_FRAME_HEADER_SIZE_BYTES      = 10
	ID3V2_MAX_FRAME_SIZE_BYTES         = 16 * 1024 * 1024
	ID3V2_APIC_FRONT_COVER             = 3
)

type id3AttachedPicture struct {
	mimeType    string
	data        []byte
	pictureType int
}

// parseID3v2Chunk reads one ID3 chunk body already positioned at its start.
func parseID3v2Chunk(file *os.File, chunkSize int64) (map[string][]string, *id3AttachedPicture, error) {
	if chunkSize < ID3V2_HEADER_SIZE_BYTES || chunkSize > ID3V2_MAX_TAG_BYTES {
		return nil, nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", fmt.Errorf("ID3 chunk size %d is invalid", chunkSize))
	}
	body := make([]byte, chunkSize)
	if _, err := io.ReadFull(file, body); err != nil {
		return nil, nil, inspectionError(INSPECTION_ERROR_FILE_READ, "file", fmt.Errorf("read ID3 chunk: %w", err))
	}
	return parseID3v2Tag(body)
}

func parseID3v2Tag(tag []byte) (map[string][]string, *id3AttachedPicture, error) {
	header, err := parseID3v2Header(tag)
	if err != nil {
		return nil, nil, err
	}
	frames := tag[ID3V2_HEADER_SIZE_BYTES : ID3V2_HEADER_SIZE_BYTES+header.tagSize]
	if header.hasFooter {
		if len(frames) < ID3V2_FOOTER_SIZE_BYTES {
			return nil, nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("ID3 footer is truncated"))
		}
		footer := frames[len(frames)-ID3V2_FOOTER_SIZE_BYTES:]
		if err := validateID3v2Footer(tag[:ID3V2_HEADER_SIZE_BYTES], footer); err != nil {
			return nil, nil, err
		}
		frames = frames[:len(frames)-ID3V2_FOOTER_SIZE_BYTES]
	}
	if header.unsynchronised {
		return nil, nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("unsynchronised ID3 tags are not supported"))
	}
	tags := make(map[string][]string)
	var artwork *id3AttachedPicture
	offset := 0
	for offset+ID3V2_FRAME_HEADER_SIZE_BYTES <= len(frames) {
		frameID := string(frames[offset : offset+4])
		if frameID == "\x00\x00\x00\x00" {
			break
		}
		if !validID3v2FrameID(frameID) {
			return nil, nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", fmt.Errorf("ID3 frame ID %q is invalid", frameID))
		}
		frameSize, err := decodeID3v2FrameSize(header.majorVersion, frames[offset+4:offset+8])
		if err != nil {
			return nil, nil, err
		}
		if frameSize <= 0 || frameSize > ID3V2_MAX_FRAME_SIZE_BYTES || offset+ID3V2_FRAME_HEADER_SIZE_BYTES+int(frameSize) > len(frames) {
			return nil, nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", fmt.Errorf("ID3 frame %q extends past the tag", frameID))
		}
		flags := binary.BigEndian.Uint16(frames[offset+8 : offset+10])
		body := frames[offset+ID3V2_FRAME_HEADER_SIZE_BYTES : offset+ID3V2_FRAME_HEADER_SIZE_BYTES+int(frameSize)]
		if err := applyID3v2Frame(header.majorVersion, frameID, flags, body, tags, &artwork); err != nil {
			return nil, nil, err
		}
		offset += ID3V2_FRAME_HEADER_SIZE_BYTES + int(frameSize)
	}
	for _, value := range frames[offset:] {
		if value != 0 {
			return nil, nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("ID3 frame data ends with malformed padding"))
		}
	}
	return tags, artwork, nil
}

func validID3v2FrameID(frameID string) bool {
	if len(frameID) != 4 {
		return false
	}
	for _, value := range frameID {
		if (value < 'A' || value > 'Z') && (value < '0' || value > '9') {
			return false
		}
	}
	return true
}

type id3v2Header struct {
	majorVersion   byte
	unsynchronised bool
	hasFooter      bool
	tagSize        int
}

func parseID3v2Header(tag []byte) (*id3v2Header, error) {
	if len(tag) < ID3V2_HEADER_SIZE_BYTES || string(tag[:3]) != ID3V2_FILE_SIGNATURE {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("ID3 signature is missing"))
	}
	header := &id3v2Header{majorVersion: tag[3]}
	if header.majorVersion < ID3V2_MAJOR_VERSION_MIN || header.majorVersion > ID3V2_MAJOR_VERSION_MAX {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", fmt.Errorf("ID3 version 2.%d is unsupported", header.majorVersion))
	}
	if tag[4] != 0x00 {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("ID3 revision must be zero"))
	}
	flags := tag[5]
	if flags&ID3V2_UNSYNCHRONISATION_FLAG != 0 {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("unsynchronised ID3 tags are not supported"))
	}
	if flags&ID3V2_EXTENDED_HEADER_FLAG != 0 {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("extended ID3 headers are not supported"))
	}
	if flags&ID3V2_EXPERIMENTAL_FLAG != 0 {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("experimental ID3 tags are not supported"))
	}
	if header.majorVersion != ID3V2_MAJOR_VERSION_MAX && flags&ID3V2_FOOTER_FLAG != 0 {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("ID3v2.3 tags cannot contain a footer"))
	}
	if flags&0x0f != 0 {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", fmt.Errorf("ID3 header uses unsupported flags 0x%02x", flags))
	}
	header.unsynchronised = false
	header.hasFooter = flags&ID3V2_FOOTER_FLAG != 0
	tagSize, err := decodeID3v2SyncsafeInt(tag[6:10])
	if err != nil {
		return nil, err
	}
	header.tagSize = tagSize
	if header.tagSize <= 0 || ID3V2_HEADER_SIZE_BYTES+header.tagSize != len(tag) {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("ID3 tag size is invalid"))
	}
	return header, nil
}

func decodeID3v2SyncsafeInt(value []byte) (int, error) {
	if len(value) != 4 {
		return 0, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("ID3 syncsafe integer is truncated"))
	}
	for _, part := range value {
		if part&0x80 != 0 {
			return 0, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("ID3 syncsafe integer has a high bit set"))
		}
	}
	return int(value[0])<<21 | int(value[1])<<14 | int(value[2])<<7 | int(value[3]), nil
}

func decodeID3v2FrameSize(majorVersion byte, value []byte) (int64, error) {
	if len(value) != 4 {
		return 0, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("ID3 frame size field is truncated"))
	}
	if majorVersion == ID3V2_MAJOR_VERSION_MAX {
		frameSize, err := decodeID3v2SyncsafeInt(value)
		return int64(frameSize), err
	}
	return int64(binary.BigEndian.Uint32(value)), nil
}

func validateID3v2Footer(header, footer []byte) error {
	if len(footer) != ID3V2_FOOTER_SIZE_BYTES || string(footer[:3]) != "3DI" || !bytes.Equal(header[3:], footer[3:]) {
		return inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("ID3 footer does not match its header"))
	}
	return nil
}

func applyID3v2Frame(majorVersion byte, frameID string, flags uint16, body []byte, tags map[string][]string, artwork **id3AttachedPicture) error {
	key := id3v2TagKey(frameID)
	if key == "" && frameID != "APIC" {
		// Non-identity frames are ignored, matching how FLAC Vorbis comments
		// may carry arbitrary fields without failing inspection.
		return nil
	}
	if flags != 0 {
		return inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", fmt.Errorf("ID3 frame %q uses unsupported flags 0x%04x", frameID, flags))
	}
	if frameID == "APIC" {
		picture, err := parseAPICFrame(majorVersion, body)
		if err != nil {
			return err
		}
		if picture.pictureType == ID3V2_APIC_FRONT_COVER {
			if *artwork != nil {
				return inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("multiple front covers are ambiguous"))
			}
			*artwork = picture
		}
		return nil
	}
	values, err := decodeID3v2TextValues(majorVersion, body)
	if err != nil {
		return err
	}
	if len(tags[key]) > 0 {
		return inspectionError(INSPECTION_ERROR_INVALID_METADATA, key, fmt.Errorf("multiple ID3 %s frames are ambiguous", frameID))
	}
	tags[key] = append(tags[key], values...)
	return nil
}

func id3v2TagKey(frameID string) string {
	switch frameID {
	case "TIT2":
		return "TITLE"
	case "TPE1":
		return "ARTIST"
	case "TPE2":
		return "ALBUMARTIST"
	case "TALB":
		return "ALBUM"
	case "TPOS":
		return "DISCNUMBER"
	case "TRCK":
		return "TRACKNUMBER"
	case "TCON":
		return "GENRE"
	case "TDRC", "TYER":
		return "DATE"
	case "TXXX":
		return ""
	default:
		return ""
	}
}

func decodeID3v2TextValues(majorVersion byte, body []byte) ([]string, error) {
	if len(body) == 0 {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("ID3 text frame is empty"))
	}
	encoding := body[0]
	if majorVersion == ID3V2_MAJOR_VERSION_MIN && encoding > 0x01 {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", fmt.Errorf("ID3v2.3 text encoding 0x%02x is unsupported", encoding))
	}
	text := body[1:]
	switch encoding {
	case 0x00:
		return decodeID3v2SingleByteValues(text, false)
	case 0x01:
		return decodeID3v2UTF16Values(text, true)
	case 0x02:
		return decodeID3v2UTF16Values(text, false)
	case 0x03:
		return decodeID3v2SingleByteValues(text, true)
	default:
		return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", fmt.Errorf("ID3 text encoding 0x%02x is unsupported", encoding))
	}
}

func decodeID3v2SingleByteValues(text []byte, isUTF8 bool) ([]string, error) {
	parts := bytes.Split(text, []byte{0x00})
	if len(parts) > 1 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	}
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if isUTF8 {
			if !utf8.Valid(part) {
				return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("ID3 UTF-8 text is invalid"))
			}
			values = append(values, string(part))
			continue
		}
		runes := make([]rune, len(part))
		for index, value := range part {
			runes[index] = rune(value)
		}
		values = append(values, string(runes))
	}
	return values, nil
}

func decodeID3v2UTF16Values(text []byte, hasBOM bool) ([]string, error) {
	if len(text)%2 != 0 {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("ID3 UTF-16 text has an odd byte count"))
	}
	var byteOrder binary.ByteOrder = binary.BigEndian
	if hasBOM {
		if len(text) < 2 {
			return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("ID3 UTF-16 text is missing its byte-order mark"))
		}
		switch {
		case text[0] == 0xfe && text[1] == 0xff:
			text = text[2:]
		case text[0] == 0xff && text[1] == 0xfe:
			byteOrder = binary.LittleEndian
			text = text[2:]
		default:
			return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("ID3 UTF-16 text is missing its byte-order mark"))
		}
	}
	parts := splitID3v2UTF16Text(text)
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		units := make([]uint16, 0, len(part)/2)
		for offset := 0; offset < len(part); offset += 2 {
			units = append(units, byteOrder.Uint16(part[offset:offset+2]))
		}
		if !validID3v2UTF16(units) {
			return nil, inspectionError(INSPECTION_ERROR_INVALID_METADATA, "metadata", errors.New("ID3 UTF-16 text contains an invalid surrogate pair"))
		}
		values = append(values, string(utf16.Decode(units)))
	}
	return values, nil
}

func splitID3v2UTF16Text(text []byte) [][]byte {
	parts := make([][]byte, 0, 1)
	start := 0
	for offset := 0; offset+1 < len(text); offset += 2 {
		if text[offset] == 0 && text[offset+1] == 0 {
			parts = append(parts, text[start:offset])
			start = offset + 2
		}
	}
	if start < len(text) || len(parts) == 0 {
		parts = append(parts, text[start:])
	}
	return parts
}

func validID3v2UTF16(units []uint16) bool {
	for index := 0; index < len(units); index++ {
		if units[index] >= 0xd800 && units[index] <= 0xdbff {
			if index+1 >= len(units) || units[index+1] < 0xdc00 || units[index+1] > 0xdfff {
				return false
			}
			index++
		} else if units[index] >= 0xdc00 && units[index] <= 0xdfff {
			return false
		}
	}
	return true
}

func parseAPICFrame(majorVersion byte, body []byte) (*id3AttachedPicture, error) {
	if len(body) == 0 {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("ID3 APIC frame is empty"))
	}
	encoding := body[0]
	if encoding > 0x03 || (majorVersion == ID3V2_MAJOR_VERSION_MIN && encoding > 0x01) {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", fmt.Errorf("ID3 APIC text encoding 0x%02x is unsupported", encoding))
	}
	rest := body[1:]
	mimeEnd := indexOfZero(rest, 1)
	if mimeEnd < 0 {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("ID3 APIC MIME type is unterminated"))
	}
	mimeType := string(rest[:mimeEnd])
	rest = rest[mimeEnd+1:]
	if len(rest) < 1 {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("ID3 APIC picture type is missing"))
	}
	pictureType := rest[0]
	rest = rest[1:]
	terminatorSize := id3v2TerminatorSize(encoding)
	descriptionEnd := indexOfZero(rest, terminatorSize)
	if descriptionEnd < 0 {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("ID3 APIC description is unterminated"))
	}
	data := rest[descriptionEnd+terminatorSize:]
	if len(data) == 0 {
		return nil, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("ID3 APIC image data is empty"))
	}
	return &id3AttachedPicture{mimeType: mimeType, data: append([]byte(nil), data...), pictureType: int(pictureType)}, nil
}

func id3v2TerminatorSize(encoding byte) int {
	if encoding == 0x01 || encoding == 0x02 {
		return 2
	}
	return 1
}

func indexOfZero(data []byte, terminatorSize int) int {
	for offset := 0; offset+terminatorSize <= len(data); offset += terminatorSize {
		isZero := true
		for i := 0; i < terminatorSize; i++ {
			if data[offset+i] != 0x00 {
				isZero = false
				break
			}
		}
		if isZero {
			return offset
		}
	}
	return -1
}

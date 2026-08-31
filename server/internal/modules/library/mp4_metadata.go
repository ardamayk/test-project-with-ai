package library

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	MP4_ATOM_HEADER_SIZE_BYTES          = 8
	MP4_EXTENDED_ATOM_HEADER_SIZE_BYTES = 16
	MP4_FULL_BOX_HEADER_SIZE_BYTES      = 4
	MP4_DATA_HEADER_SIZE_BYTES          = 8
	MAX_MP4_ATOM_DEPTH                  = 8
)

type parsedMP4Atom struct {
	name      string
	dataStart int64
	end       int64
}

func readMP4StructuredCredits(path string) (credits map[string][]string, returnErr error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open MP4 metadata: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close MP4 metadata: %w", closeErr))
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat MP4 metadata: %w", err)
	}
	credits = make(map[string][]string)
	for offset := int64(0); offset < info.Size(); {
		atom, err := readMP4Atom(file, offset, info.Size())
		if err != nil {
			return nil, err
		}
		if atom.name == "moov" {
			if err := walkMP4Metadata(file, atom.dataStart, atom.end, 1, credits); err != nil {
				return nil, err
			}
			return credits, nil
		}
		offset = atom.end
	}
	return nil, errors.New("MP4 metadata atom is missing")
}

func walkMP4Metadata(reader io.ReaderAt, start, end int64, depth int, credits map[string][]string) error {
	if depth > MAX_MP4_ATOM_DEPTH {
		return errors.New("MP4 metadata nesting exceeds limit")
	}
	for offset := start; offset < end; {
		atom, err := readMP4Atom(reader, offset, end)
		if err != nil {
			return err
		}
		if atom.name == "----" {
			if err := readMP4FreeformCredit(reader, atom, credits); err != nil {
				return err
			}
		}
		if isMP4MetadataContainer(atom.name) {
			childStart := atom.dataStart
			if atom.name == "meta" {
				childStart += MP4_FULL_BOX_HEADER_SIZE_BYTES
			}
			if childStart > atom.end {
				return errors.New("MP4 metadata container is truncated")
			}
			if err := walkMP4Metadata(reader, childStart, atom.end, depth+1, credits); err != nil {
				return err
			}
		}
		offset = atom.end
	}
	return nil
}

func readMP4Atom(reader io.ReaderAt, offset, parentEnd int64) (parsedMP4Atom, error) {
	var header [MP4_EXTENDED_ATOM_HEADER_SIZE_BYTES]byte
	if parentEnd-offset < MP4_ATOM_HEADER_SIZE_BYTES {
		return parsedMP4Atom{}, errors.New("MP4 atom header is truncated")
	}
	if _, err := reader.ReadAt(header[:MP4_ATOM_HEADER_SIZE_BYTES], offset); err != nil {
		return parsedMP4Atom{}, fmt.Errorf("read MP4 atom header: %w", err)
	}
	size := int64(binary.BigEndian.Uint32(header[:4]))
	headerSize := int64(MP4_ATOM_HEADER_SIZE_BYTES)
	switch size {
	case 1:
		if parentEnd-offset < MP4_EXTENDED_ATOM_HEADER_SIZE_BYTES {
			return parsedMP4Atom{}, errors.New("extended MP4 atom header is truncated")
		}
		if _, err := reader.ReadAt(header[MP4_ATOM_HEADER_SIZE_BYTES:], offset+MP4_ATOM_HEADER_SIZE_BYTES); err != nil {
			return parsedMP4Atom{}, fmt.Errorf("read extended MP4 atom header: %w", err)
		}
		extendedSize := binary.BigEndian.Uint64(header[MP4_ATOM_HEADER_SIZE_BYTES:])
		if extendedSize > uint64(parentEnd-offset) {
			return parsedMP4Atom{}, errors.New("extended MP4 atom exceeds parent")
		}
		size = int64(extendedSize)
		headerSize = MP4_EXTENDED_ATOM_HEADER_SIZE_BYTES
	case 0:
		size = parentEnd - offset
	}
	if size < headerSize || size > parentEnd-offset {
		return parsedMP4Atom{}, errors.New("MP4 atom size is invalid")
	}
	return parsedMP4Atom{name: string(header[4:8]), dataStart: offset + headerSize, end: offset + size}, nil
}

func isMP4MetadataContainer(name string) bool {
	return name == "moov" || name == "udta" || name == "meta" || name == "ilst"
}

func readMP4FreeformCredit(reader io.ReaderAt, atom parsedMP4Atom, credits map[string][]string) error {
	var mean, name string
	var values []string
	for offset := atom.dataStart; offset < atom.end; {
		subAtom, err := readMP4Atom(reader, offset, atom.end)
		if err != nil {
			return err
		}
		switch subAtom.name {
		case "mean":
			value, err := readMP4AtomString(reader, subAtom)
			if err != nil {
				return err
			}
			mean = value
		case "name":
			value, err := readMP4AtomString(reader, subAtom)
			if err != nil {
				return err
			}
			name = value
		case "data":
			if mean == "com.apple.iTunes" && (name == "ARTISTS" || name == "ALBUMARTISTS") {
				value, err := readMP4AtomString(reader, subAtom)
				if err != nil {
					return err
				}
				values = append(values, value)
			}
		}
		offset = subAtom.end
	}
	if mean == "com.apple.iTunes" && (name == "ARTISTS" || name == "ALBUMARTISTS") {
		credits[name] = append(credits[name], values...)
	}
	return nil
}

func readMP4AtomString(reader io.ReaderAt, atom parsedMP4Atom) (string, error) {
	headerSize := int64(MP4_FULL_BOX_HEADER_SIZE_BYTES)
	if atom.name == "data" {
		headerSize = MP4_DATA_HEADER_SIZE_BYTES
	}
	size := atom.end - atom.dataStart - headerSize
	if size < 0 || size > MAX_IDENTITY_VALUE_BYTES {
		return "", errors.New("MP4 metadata value size is invalid")
	}
	value := make([]byte, size)
	if _, err := reader.ReadAt(value, atom.dataStart+headerSize); err != nil {
		return "", fmt.Errorf("read MP4 metadata value: %w", err)
	}
	return string(value), nil
}

package library

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	APEV2_SIGNATURE              = "APETAGEX"
	APEV2_DESCRIPTOR_SIZE        = 32
	APEV2_VERSION                = 2000
	APEV2_HAS_HEADER      uint32 = 1 << 31
	APEV2_IS_HEADER       uint32 = 1 << 29
	APEV2_ITEM_FLAGS      uint32 = 7
	APEV2_MIN_ITEM_SIZE          = 11
)

// Only trailing APEv2 boundaries are inspected; ID3 remains the metadata source.
func mp3APEv2AudioEnd(ctx context.Context, file *os.File, audioOffset, audioEnd int64) (int64, error) {
	if audioEnd-audioOffset < APEV2_DESCRIPTOR_SIZE {
		return audioEnd, nil
	}
	var footer [APEV2_DESCRIPTOR_SIZE]byte
	if _, err := file.ReadAt(footer[:], audioEnd-APEV2_DESCRIPTOR_SIZE); err != nil {
		return 0, fmt.Errorf("read APEv2 footer: %w", err)
	}
	if string(footer[:len(APEV2_SIGNATURE)]) != APEV2_SIGNATURE {
		return audioEnd, nil
	}
	if err := validateAPEv2Descriptor(footer[:], false); err != nil {
		return 0, err
	}
	return validateAPEv2Bounds(ctx, file, audioOffset, audioEnd, footer[:])
}

func validateAPEv2Bounds(ctx context.Context, file *os.File, audioOffset, audioEnd int64, footer []byte) (int64, error) {
	size := int64(binary.LittleEndian.Uint32(footer[12:16]))
	count := int64(binary.LittleEndian.Uint32(footer[16:20]))
	if size < APEV2_DESCRIPTOR_SIZE || size > audioEnd-audioOffset || count > (size-APEV2_DESCRIPTOR_SIZE)/APEV2_MIN_ITEM_SIZE {
		return 0, errors.New("APEv2 size or item count exceeds tag boundaries")
	}
	start := audioEnd - size
	if err := validateAPEv2Items(ctx, file, start, size-APEV2_DESCRIPTOR_SIZE, count); err != nil {
		return 0, err
	}
	if binary.LittleEndian.Uint32(footer[20:24])&APEV2_HAS_HEADER != 0 {
		start -= APEV2_DESCRIPTOR_SIZE
		if start < audioOffset {
			return 0, errors.New("APEv2 header overlaps ID3 or audio start")
		}
		if err := validateAPEv2Header(file, start, footer[:]); err != nil {
			return 0, err
		}
	}
	return start, nil
}

func validateAPEv2Descriptor(data []byte, isHeader bool) error {
	flags := binary.LittleEndian.Uint32(data[20:24])
	if string(data[:len(APEV2_SIGNATURE)]) != APEV2_SIGNATURE || binary.LittleEndian.Uint32(data[8:12]) != APEV2_VERSION {
		return errors.New("APEv2 signature or version is invalid")
	}
	allowedFlags := APEV2_HAS_HEADER | APEV2_IS_HEADER | APEV2_ITEM_FLAGS
	if flags & ^allowedFlags != 0 || (flags&APEV2_IS_HEADER != 0) != isHeader || flags&6 == 6 || !isZeroPadding(data[24:]) {
		return errors.New("APEv2 flags or reserved bytes are invalid")
	}
	return nil
}

func validateAPEv2Header(file *os.File, offset int64, footer []byte) error {
	var header [APEV2_DESCRIPTOR_SIZE]byte
	if _, err := file.ReadAt(header[:], offset); err != nil {
		return fmt.Errorf("read APEv2 header: %w", err)
	}
	if err := validateAPEv2Descriptor(header[:], true); err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(header[20:24], binary.LittleEndian.Uint32(header[20:24]) & ^APEV2_IS_HEADER)
	if !bytes.Equal(header[:], footer) {
		return errors.New("APEv2 header and footer disagree")
	}
	return nil
}

// Walk item lengths without interpreting values or allocating tag-sized buffers.
func validateAPEv2Items(ctx context.Context, file *os.File, offset, size, count int64) error {
	reader := bufio.NewReader(contextReader{ctx: ctx, reader: io.NewSectionReader(file, offset, size)})
	for range count {
		var header [8]byte
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			return fmt.Errorf("read APEv2 item header: %w", err)
		}
		flags := binary.LittleEndian.Uint32(header[4:])
		if flags & ^APEV2_ITEM_FLAGS != 0 || flags&6 == 6 {
			return errors.New("APEv2 item flags are invalid")
		}
		if err := readAPEv2Key(reader); err != nil {
			return err
		}
		valueSize := int64(binary.LittleEndian.Uint32(header[:4]))
		if _, err := io.CopyN(io.Discard, reader, valueSize); err != nil {
			return fmt.Errorf("APEv2 item exceeds tag boundaries: %w", err)
		}
	}
	_, err := reader.ReadByte()
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read APEv2 item boundary: %w", err)
	}
	return errors.New("APEv2 item count does not match tag size")
}

func readAPEv2Key(reader *bufio.Reader) error {
	const MIN_KEY_LENGTH = 2
	const MAX_KEY_LENGTH = 255
	for length := 0; length <= MAX_KEY_LENGTH; length++ {
		value, err := reader.ReadByte()
		if err != nil {
			return fmt.Errorf("APEv2 item key is truncated: %w", err)
		}
		if value == 0 && length >= MIN_KEY_LENGTH {
			return nil
		}
		if value < ' ' || value > '~' {
			return errors.New("APEv2 item key is invalid")
		}
	}
	return errors.New("APEv2 item key exceeds length limit")
}

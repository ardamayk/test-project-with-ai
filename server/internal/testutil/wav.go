package testutil

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

type WAVFixture struct {
	AudioFormat  uint16
	ChannelCount uint16
	SampleRateHz uint32
	BitDepth     uint16
	PCMFrames    int
	ID3Frames    []WAVID3Frame
	OmitID3      bool
	RIFFSizeFix  func(uint32) uint32
}

type WAVID3Frame struct {
	ID   string
	Body []byte
}

func WAVTextFrame(id, value string) WAVID3Frame {
	return WAVID3Frame{ID: id, Body: append([]byte{0x03}, []byte(value)...)}
}

func WAVAPICFrame(pictureType int, mimeType, description string, data []byte) WAVID3Frame {
	body := []byte{0x00}
	body = append(body, []byte(mimeType)...)
	body = append(body, 0x00, byte(pictureType))
	body = append(body, []byte(description)...)
	body = append(body, 0x00)
	body = append(body, data...)
	return WAVID3Frame{ID: "APIC", Body: body}
}

func EncodeWAV(t testing.TB, fixture WAVFixture) []byte {
	t.Helper()
	format := encodeWAVFormat(fixture)
	blockAlign := int(fixture.ChannelCount) * int(fixture.BitDepth) / 8
	pcm := make([]byte, fixture.PCMFrames*blockAlign)
	for index := range pcm {
		pcm[index] = byte(index % 251)
	}

	var body bytes.Buffer
	appendWAVChunk(t, &body, "fmt ", format)
	if !fixture.OmitID3 {
		appendWAVChunk(t, &body, "ID3 ", encodeWAVID3(t, fixture.ID3Frames))
	}
	appendWAVChunk(t, &body, "data", pcm)

	riffSize := uint32(4 + body.Len())
	if fixture.RIFFSizeFix != nil {
		riffSize = fixture.RIFFSizeFix(riffSize)
	}
	var output bytes.Buffer
	output.WriteString("RIFF")
	writeWAVBinary(t, &output, binary.LittleEndian, riffSize)
	output.WriteString("WAVE")
	output.Write(body.Bytes())
	return output.Bytes()
}

func WAVCoverPNG() []byte {
	cover := image.NewRGBA(image.Rect(0, 0, 2, 2))
	cover.Set(0, 0, color.RGBA{R: 255, A: 255})
	cover.Set(1, 1, color.RGBA{B: 255, A: 255})
	var output bytes.Buffer
	if err := png.Encode(&output, cover); err != nil {
		panic(err)
	}
	return output.Bytes()
}

func encodeWAVFormat(fixture WAVFixture) []byte {
	formatSize := 16
	if fixture.AudioFormat == 0xfffe {
		formatSize = 40
	}
	format := make([]byte, formatSize)
	blockAlign := fixture.ChannelCount * fixture.BitDepth / 8
	binary.LittleEndian.PutUint16(format[0:2], fixture.AudioFormat)
	binary.LittleEndian.PutUint16(format[2:4], fixture.ChannelCount)
	binary.LittleEndian.PutUint32(format[4:8], fixture.SampleRateHz)
	binary.LittleEndian.PutUint32(format[8:12], fixture.SampleRateHz*uint32(blockAlign))
	binary.LittleEndian.PutUint16(format[12:14], blockAlign)
	binary.LittleEndian.PutUint16(format[14:16], fixture.BitDepth)
	if fixture.AudioFormat == 0xfffe {
		binary.LittleEndian.PutUint16(format[16:18], 22)
		binary.LittleEndian.PutUint16(format[18:20], fixture.BitDepth)
		copy(format[24:40], []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71})
	}
	return format
}

func appendWAVChunk(t testing.TB, output *bytes.Buffer, id string, body []byte) {
	t.Helper()
	output.WriteString(id)
	writeWAVBinary(t, output, binary.LittleEndian, uint32(len(body)))
	output.Write(body)
	if len(body)%2 == 1 {
		output.WriteByte(0x00)
	}
}

func encodeWAVID3(t testing.TB, frames []WAVID3Frame) []byte {
	t.Helper()
	var frameBytes bytes.Buffer
	for _, frame := range frames {
		frameBytes.WriteString(frame.ID)
		size := len(frame.Body)
		frameBytes.Write([]byte{byte(size >> 21 & 0x7f), byte(size >> 14 & 0x7f), byte(size >> 7 & 0x7f), byte(size & 0x7f)})
		writeWAVBinary(t, &frameBytes, binary.BigEndian, uint16(0))
		frameBytes.Write(frame.Body)
	}
	tagSize := frameBytes.Len()
	var output bytes.Buffer
	output.WriteString("ID3")
	output.Write([]byte{0x04, 0x00, 0x00})
	output.Write([]byte{byte(tagSize >> 21 & 0x7f), byte(tagSize >> 14 & 0x7f), byte(tagSize >> 7 & 0x7f), byte(tagSize & 0x7f)})
	output.Write(frameBytes.Bytes())
	return output.Bytes()
}

func writeWAVBinary(t testing.TB, output *bytes.Buffer, byteOrder binary.ByteOrder, value any) {
	t.Helper()
	if err := binary.Write(output, byteOrder, value); err != nil {
		t.Fatalf("encode WAV fixture: %v", err)
	}
}

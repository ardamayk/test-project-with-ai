package library

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestWalkMusicPathsReadsReplayGainFromTaggedFiles(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		data     []byte
		want     ReplayGainMetadata
	}{
		{
			name:     "full FLAC Vorbis metadata",
			fileName: "full.flac",
			data: flacWithVorbisComments(
				"REPLAYGAIN_TRACK_GAIN=-7.25 dB",
				"REPLAYGAIN_TRACK_PEAK=0.987654",
				"REPLAYGAIN_ALBUM_GAIN=-6.50 dB",
				"REPLAYGAIN_ALBUM_PEAK=1.012345",
			),
			want: ReplayGainMetadata{
				TrackGainDB: float64Pointer(-7.25),
				TrackPeak:   float64Pointer(0.987654),
				AlbumGainDB: float64Pointer(-6.5),
				AlbumPeak:   float64Pointer(1.012345),
			},
		},
		{
			name:     "partial ID3 metadata",
			fileName: "partial.mp3",
			data: id3WithReplayGain(
				[2]string{"REPLAYGAIN_TRACK_GAIN", "+1.75 dB"},
				[2]string{"REPLAYGAIN_ALBUM_PEAK", "0.95"},
			),
			want: ReplayGainMetadata{
				TrackGainDB: float64Pointer(1.75),
				AlbumPeak:   float64Pointer(0.95),
			},
		},
		{
			name:     "malformed MP4 metadata",
			fileName: "malformed.m4a",
			data: mp4WithReplayGain(
				[2]string{"REPLAYGAIN_TRACK_GAIN", "loud"},
				[2]string{"REPLAYGAIN_TRACK_PEAK", "-0.1"},
				[2]string{"REPLAYGAIN_ALBUM_GAIN", "+Inf dB"},
				[2]string{"REPLAYGAIN_ALBUM_PEAK", "NaN"},
			),
			want: ReplayGainMetadata{},
		},
		{
			name:     "absent FLAC metadata",
			fileName: "absent.flac",
			data:     flacWithVorbisComments("TITLE=No ReplayGain"),
			want:     ReplayGainMetadata{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, test.fileName)
			if err := os.WriteFile(path, test.data, 0o644); err != nil {
				t.Fatal(err)
			}

			files, err := WalkMusicPaths([]string{directory})
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 1 {
				t.Fatalf("files = %d, want 1", len(files))
			}
			assertReplayGainMetadata(t, files[0].Metadata.ReplayGain, test.want)
		})
	}
}

func flacWithVorbisComments(comments ...string) []byte {
	payload := appendLittleEndianString(nil, "test")
	payload = binary.LittleEndian.AppendUint32(payload, uint32(len(comments)))
	for _, comment := range comments {
		payload = appendLittleEndianString(payload, comment)
	}

	data := append([]byte("fLaC"), byte(0x80|4))
	length := len(payload)
	data = append(data, byte(length>>16), byte(length>>8), byte(length))
	return append(data, payload...)
}

func appendLittleEndianString(data []byte, value string) []byte {
	data = binary.LittleEndian.AppendUint32(data, uint32(len(value)))
	return append(data, value...)
}

func id3WithReplayGain(values ...[2]string) []byte {
	frames := []byte{}
	for _, value := range values {
		payload := append([]byte{0}, []byte(value[0])...)
		payload = append(payload, 0)
		payload = append(payload, []byte(value[1])...)
		frames = append(frames, "TXXX"...)
		frames = binary.BigEndian.AppendUint32(frames, uint32(len(payload)))
		frames = append(frames, 0, 0)
		frames = append(frames, payload...)
	}

	header := append([]byte("ID3"), 3, 0, 0)
	header = append(header, synchsafe(len(frames))...)
	return append(header, frames...)
}

func synchsafe(value int) []byte {
	return []byte{
		byte(value >> 21),
		byte(value >> 14 & 0x7f),
		byte(value >> 7 & 0x7f),
		byte(value & 0x7f),
	}
}

func mp4WithReplayGain(values ...[2]string) []byte {
	customAtoms := []byte{}
	for _, value := range values {
		customAtoms = append(customAtoms, mp4CustomAtom(value[0], value[1])...)
	}
	ilst := mp4Atom("ilst", customAtoms)
	meta := mp4Atom("meta", append([]byte{0, 0, 0, 0}, ilst...))
	moov := mp4Atom("moov", mp4Atom("udta", meta))
	ftyp := mp4Atom("ftyp", []byte("M4A \x00\x00\x00\x00"))
	return append(ftyp, moov...)
}

func mp4CustomAtom(name, value string) []byte {
	mean := mp4Atom("mean", append([]byte{0, 0, 0, 0}, []byte("com.apple.iTunes")...))
	nameAtom := mp4Atom("name", append([]byte{0, 0, 0, 0}, []byte(name)...))
	data := mp4Atom("data", append([]byte{0, 0, 0, 0}, []byte(value)...))
	payload := append(mean, nameAtom...)
	payload = append(payload, data...)
	return mp4Atom("----", payload)
}

func mp4Atom(name string, payload []byte) []byte {
	atom := binary.BigEndian.AppendUint32(nil, uint32(len(payload)+8))
	atom = append(atom, name...)
	return append(atom, payload...)
}

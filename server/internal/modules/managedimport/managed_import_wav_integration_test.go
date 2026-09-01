package managedimport_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/modules/managedimport"
	"github.com/ardam/navidrome-replacement/server/internal/modules/playback"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

const (
	WAV_IMPORT_SAMPLE_RATE_HZ = 48000
	WAV_IMPORT_CHANNELS       = 2
	WAV_IMPORT_BIT_DEPTH      = 24
	WAV_IMPORT_PCM_FRAMES     = 4800
)

// buildStrictWAV produces a fully tagged, artwork-bearing integer-PCM WAV that
// satisfies the Strict Import Profile.
func buildStrictWAV(t *testing.T) []byte {
	t.Helper()
	blockAlign := WAV_IMPORT_CHANNELS * WAV_IMPORT_BIT_DEPTH / 8
	fmtBody := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtBody[0:2], 1)
	binary.LittleEndian.PutUint16(fmtBody[2:4], WAV_IMPORT_CHANNELS)
	binary.LittleEndian.PutUint32(fmtBody[4:8], WAV_IMPORT_SAMPLE_RATE_HZ)
	binary.LittleEndian.PutUint32(fmtBody[8:12], WAV_IMPORT_SAMPLE_RATE_HZ*uint32(blockAlign))
	binary.LittleEndian.PutUint16(fmtBody[12:14], uint16(blockAlign))
	binary.LittleEndian.PutUint16(fmtBody[14:16], WAV_IMPORT_BIT_DEPTH)

	pcm := make([]byte, WAV_IMPORT_PCM_FRAMES*blockAlign)
	for i := range pcm {
		pcm[i] = byte(i % 251)
	}

	tag := encodeWAVID3Tag(t, [][2]string{
		{"TIT2", "WAV Fixture"},
		{"TPE1", "WAV Artist"},
		{"TPE2", "WAV Album Artist"},
		{"TALB", "WAV Strict Import"},
		{"TRCK", "3/9"},
		{"TPOS", "1/1"},
		{"TCON", "Ambient"},
		{"TDRC", "2026"},
	}, encodeWAVCoverPNG(t))

	var body bytes.Buffer
	appendWAVChunk := func(id string, chunk []byte) {
		body.WriteString(id)
		binary.Write(&body, binary.LittleEndian, uint32(len(chunk)))
		body.Write(chunk)
		if len(chunk)%2 == 1 {
			body.WriteByte(0x00)
		}
	}
	appendWAVChunk("fmt ", fmtBody)
	appendWAVChunk("ID3 ", tag)
	appendWAVChunk("data", pcm)

	var output bytes.Buffer
	output.WriteString("RIFF")
	binary.Write(&output, binary.LittleEndian, uint32(4+body.Len()))
	output.WriteString("WAVE")
	output.Write(body.Bytes())
	return output.Bytes()
}

func encodeWAVCoverPNG(t *testing.T) []byte {
	t.Helper()
	cover := image.NewRGBA(image.Rect(0, 0, 2, 2))
	cover.Set(0, 0, color.RGBA{R: 255, A: 255})
	cover.Set(1, 1, color.RGBA{G: 255, A: 255})
	var output bytes.Buffer
	if err := png.Encode(&output, cover); err != nil {
		t.Fatalf("encode WAV cover: %v", err)
	}
	return output.Bytes()
}

func encodeWAVID3Tag(t *testing.T, tags [][2]string, artwork []byte) []byte {
	t.Helper()
	writeFrame := func(buffer *bytes.Buffer, id string, frameBody []byte) {
		buffer.WriteString(id)
		size := len(frameBody)
		buffer.Write([]byte{
			byte(size >> 21 & 0x7f),
			byte(size >> 14 & 0x7f),
			byte(size >> 7 & 0x7f),
			byte(size & 0x7f),
		})
		binary.Write(buffer, binary.BigEndian, uint16(0))
		buffer.Write(frameBody)
	}
	var frameBytes bytes.Buffer
	for _, pair := range tags {
		writeFrame(&frameBytes, pair[0], append([]byte{0x03}, []byte(pair[1])...))
	}
	var pictureBody []byte
	pictureBody = append(pictureBody, 0x00)
	pictureBody = append(pictureBody, []byte("image/png")...)
	pictureBody = append(pictureBody, 0x00)
	pictureBody = append(pictureBody, byte(3))
	pictureBody = append(pictureBody, []byte("cover")...)
	pictureBody = append(pictureBody, 0x00)
	pictureBody = append(pictureBody, artwork...)
	writeFrame(&frameBytes, "APIC", pictureBody)

	tagSize := frameBytes.Len()
	var output bytes.Buffer
	output.WriteString("ID3")
	output.Write([]byte{0x04, 0x00, 0x00})
	output.Write([]byte{
		byte(tagSize >> 21 & 0x7f),
		byte(tagSize >> 14 & 0x7f),
		byte(tagSize >> 7 & 0x7f),
		byte(tagSize & 0x7f),
	})
	output.Write(frameBytes.Bytes())
	return output.Bytes()
}

func TestManagedImportCommitsStrictWAVThroughLibraryPlayback(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	managedStoragePath := t.TempDir()
	configuration := config.Config{
		ManagedStoragePath: managedStoragePath,
		MusicPaths:         []string{t.TempDir()},
	}
	libraryModule := library.NewModule(database, configuration)
	importModule := managedimport.NewModule(database, configuration, library.NewMediaInspector())
	playbackModule := playback.NewModule(database, libraryModule.TrackAccess())
	router := chi.NewRouter()
	importModule.RegisterRoutes(router)
	libraryModule.RegisterRoutes(router)
	playbackModule.RegisterRoutes(router)
	fixture := buildStrictWAV(t)

	jobResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/imports", nil, nil)
	var job struct {
		ID string `json:"id"`
	}
	testutil.DecodeJSON(t, jobResponse, &job)
	uploadResponse := testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+job.ID+"/file", bytes.NewReader(fixture), map[string]string{
		"Content-Type":      "audio/wav",
		"X-Import-Filename": "strict-import.wav",
	})
	if uploadResponse.Code != http.StatusOK {
		t.Fatalf("WAV upload status = %d, body = %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	var preview struct {
		Revision int `json:"revision"`
		File     struct {
			Title            string   `json:"title"`
			Artists          []string `json:"artists"`
			AlbumArtists     []string `json:"albumArtists"`
			Album            string   `json:"album"`
			Genres           []string `json:"genres"`
			TrackNo          int      `json:"trackNo"`
			Format           string   `json:"format"`
			Container        string   `json:"container"`
			Codec            string   `json:"codec"`
			DurationMs       int      `json:"durationMs"`
			SampleRateHz     int      `json:"sampleRateHz"`
			ChannelCount     int      `json:"channelCount"`
			BitDepth         int      `json:"bitDepth"`
			BitrateKbps      int      `json:"bitrateKbps"`
			ArtworkMediaType string   `json:"artworkMediaType"`
		} `json:"file"`
	}
	testutil.DecodeJSON(t, uploadResponse, &preview)
	if preview.Revision != 2 || preview.File.Title != "WAV Fixture" || preview.File.Album != "WAV Strict Import" {
		t.Fatalf("WAV Import Preview = %+v", preview)
	}
	if strings.Join(preview.File.Artists, ",") != "WAV Artist" || strings.Join(preview.File.AlbumArtists, ",") != "WAV Album Artist" || strings.Join(preview.File.Genres, ",") != "Ambient" {
		t.Fatalf("WAV preview relationships = %+v", preview.File)
	}
	if preview.File.Format != "wav" || preview.File.Container != "wav" || preview.File.Codec != "pcm_s24le" || preview.File.ArtworkMediaType != "image/png" {
		t.Fatalf("WAV preview media = %+v", preview.File)
	}
	if preview.File.TrackNo != 3 || preview.File.DurationMs != 100 || preview.File.SampleRateHz != WAV_IMPORT_SAMPLE_RATE_HZ || preview.File.ChannelCount != WAV_IMPORT_CHANNELS || preview.File.BitDepth != WAV_IMPORT_BIT_DEPTH || preview.File.BitrateKbps <= 0 {
		t.Fatalf("WAV preview technical properties = %+v", preview.File)
	}

	confirmResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/imports/"+job.ID+"/confirm", strings.NewReader(fmt.Sprintf(`{"revision":%d}`, preview.Revision)), map[string]string{"Content-Type": "application/json"})
	if confirmResponse.Code != http.StatusOK {
		t.Fatalf("WAV confirm status = %d, body = %s", confirmResponse.Code, confirmResponse.Body.String())
	}
	var result struct {
		TrackID string `json:"trackId"`
	}
	testutil.DecodeJSON(t, confirmResponse, &result)
	if result.TrackID == "" {
		t.Fatalf("WAV import result = %+v", result)
	}

	tracks := listTracks(t, router)
	if len(tracks.Items) != 1 || tracks.Items[0].ID != result.TrackID {
		t.Fatalf("WAV library tracks = %+v", tracks.Items)
	}
	track := tracks.Items[0]
	if track.Container != "wav" || track.Codec != "pcm_s24le" || track.SampleRateHz != WAV_IMPORT_SAMPLE_RATE_HZ || track.BitDepth != WAV_IMPORT_BIT_DEPTH {
		t.Fatalf("persisted WAV track = %+v", track)
	}

	streamResponse := testutil.ServeRequest(t, router, http.MethodGet, "/api/v1/tracks/"+result.TrackID+"/stream", nil, nil)
	if streamResponse.Code != http.StatusOK {
		t.Fatalf("WAV stream status = %d, body = %s", streamResponse.Code, streamResponse.Body.String())
	}
	if !bytes.Equal(streamResponse.Body.Bytes(), fixture) {
		t.Fatal("streamed WAV bytes differ from uploaded file")
	}

	audioPath := findCanonicalAudio(t, managedStoragePath)
	if audioPath == "" || !strings.HasSuffix(audioPath, ".wav") {
		t.Fatalf("canonical WAV path = %q", audioPath)
	}
	stored, err := os.ReadFile(audioPath)
	if err != nil {
		t.Fatalf("read canonical WAV: %v", err)
	}
	if !bytes.Equal(stored, fixture) {
		t.Fatal("canonical WAV bytes differ from uploaded file")
	}
}

func findCanonicalAudio(t *testing.T, root string) string {
	t.Helper()
	var audioPath string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".wav", ".flac":
			audioPath = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Managed Storage: %v", err)
	}
	return audioPath
}

func TestManagedImportRejectsUntaggedWAVWithActionableError(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	configuration := config.Config{ManagedStoragePath: t.TempDir()}
	importModule := managedimport.NewModule(database, configuration, library.NewMediaInspector())
	router := chi.NewRouter()
	importModule.RegisterRoutes(router)

	// Ordinary recorder output: fmt + data only, no ID3, no artwork.
	blockAlign := WAV_IMPORT_CHANNELS * WAV_IMPORT_BIT_DEPTH / 8
	fmtBody := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtBody[0:2], 1)
	binary.LittleEndian.PutUint16(fmtBody[2:4], WAV_IMPORT_CHANNELS)
	binary.LittleEndian.PutUint32(fmtBody[4:8], WAV_IMPORT_SAMPLE_RATE_HZ)
	binary.LittleEndian.PutUint32(fmtBody[8:12], WAV_IMPORT_SAMPLE_RATE_HZ*uint32(blockAlign))
	binary.LittleEndian.PutUint16(fmtBody[12:14], uint16(blockAlign))
	binary.LittleEndian.PutUint16(fmtBody[14:16], WAV_IMPORT_BIT_DEPTH)
	pcm := make([]byte, WAV_IMPORT_PCM_FRAMES*blockAlign)
	var body bytes.Buffer
	body.WriteString("fmt ")
	binary.Write(&body, binary.LittleEndian, uint32(len(fmtBody)))
	body.Write(fmtBody)
	body.WriteString("data")
	binary.Write(&body, binary.LittleEndian, uint32(len(pcm)))
	body.Write(pcm)
	var output bytes.Buffer
	output.WriteString("RIFF")
	binary.Write(&output, binary.LittleEndian, uint32(4+body.Len()))
	output.WriteString("WAVE")
	output.Write(body.Bytes())

	response := uploadWAVThroughRouter(t, router, output.Bytes())
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("untagged WAV status = %d, body = %s", response.Code, response.Body.String())
	}
	var failure struct {
		Code   string `json:"code"`
		Field  string `json:"field"`
		Reason string `json:"reason"`
	}
	testutil.DecodeJSON(t, response, &failure)
	if failure.Code != "invalid_metadata" || failure.Field != "metadata" {
		t.Fatalf("untagged WAV failure = %+v", failure)
	}
}

func uploadWAVThroughRouter(t *testing.T, router http.Handler, fixture []byte) *httptest.ResponseRecorder {
	t.Helper()
	jobResponse := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/imports", nil, nil)
	var job struct {
		ID string `json:"id"`
	}
	testutil.DecodeJSON(t, jobResponse, &job)
	return testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+job.ID+"/file", bytes.NewReader(fixture), map[string]string{
		"Content-Type":      "audio/wav",
		"X-Import-Filename": "recording.wav",
	})
}

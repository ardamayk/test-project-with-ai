package library

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	flacmeta "github.com/mewkiz/flac/meta"
)

const (
	FFMPEG_ERROR_OUTPUT_LIMIT_BYTES = 64 * 1024
	FFPROBE_OUTPUT_LIMIT_BYTES      = 1024 * 1024
	MILLISECONDS_PER_SECOND         = 1000
)

type m4aProbe struct {
	Streams []m4aProbeStream `json:"streams"`
	Format  m4aProbeFormat   `json:"format"`
}

type m4aProbeStream struct {
	Index            int               `json:"index"`
	CodecName        string            `json:"codec_name"`
	CodecType        string            `json:"codec_type"`
	SampleRate       string            `json:"sample_rate"`
	Channels         int               `json:"channels"`
	BitsPerRawSample string            `json:"bits_per_raw_sample"`
	BitRate          string            `json:"bit_rate"`
	Duration         string            `json:"duration"`
	Disposition      map[string]int    `json:"disposition"`
	Tags             map[string]string `json:"tags"`
}

type m4aProbeFormat struct {
	FormatName string            `json:"format_name"`
	Tags       map[string]string `json:"tags"`
}

func inspectM4A(ctx context.Context, path string, reportProgress InspectionProgressReporter) (MediaInspection, error) {
	fileHash, err := hashFile(ctx, path)
	if err != nil {
		return MediaInspection{}, err
	}
	probe, err := probeM4A(ctx, path)
	if err != nil {
		return MediaInspection{}, err
	}
	audioStream, artworkStream, err := validateM4AStreams(probe)
	if err != nil {
		return MediaInspection{}, err
	}
	metadata, err := inspectM4AMetadata(probe.Format.Tags)
	if err != nil {
		return MediaInspection{}, err
	}
	artwork, err := inspectM4AArtwork(ctx, path, artworkStream)
	if err != nil {
		return MediaInspection{}, err
	}
	audio, err := inspectM4AAudio(ctx, path, audioStream, reportProgress)
	if err != nil {
		return MediaInspection{}, err
	}
	return MediaInspection{Metadata: metadata, AlbumArtwork: artwork, Audio: audio, FileSHA256: fileHash}, nil
}

func hashFile(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", inspectionError(INSPECTION_ERROR_FILE_READ, "file", err)
	}
	fileHash, _, hashErr := hashAndRewind(ctx, file)
	closeErr := file.Close()
	if hashErr != nil {
		if errors.Is(hashErr, context.Canceled) || errors.Is(hashErr, context.DeadlineExceeded) {
			return "", inspectionCancellationError(ctx)
		}
		return "", inspectionError(INSPECTION_ERROR_FILE_READ, "file", hashErr)
	}
	if closeErr != nil {
		return "", inspectionError(INSPECTION_ERROR_FILE_READ, "file", closeErr)
	}
	return fileHash, nil
}

func probeM4A(ctx context.Context, path string) (m4aProbe, error) {
	command := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path)
	output := newBoundedBuffer(FFPROBE_OUTPUT_LIMIT_BYTES)
	command.Stdout = output
	command.Stderr = newBoundedBuffer(FFMPEG_ERROR_OUTPUT_LIMIT_BYTES)
	err := command.Run()
	if ctx.Err() != nil {
		return m4aProbe{}, inspectionCancellationError(ctx)
	}
	if err != nil {
		return m4aProbe{}, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", fmt.Errorf("probe M4A container: %w", err))
	}
	if output.isTruncated {
		return m4aProbe{}, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", errors.New("M4A probe output exceeds limit"))
	}
	var probe m4aProbe
	if err := json.Unmarshal(output.Bytes(), &probe); err != nil {
		return m4aProbe{}, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", fmt.Errorf("decode M4A probe: %w", err))
	}
	if !isM4AProbe(probe) {
		return m4aProbe{}, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "container", errors.New("M4A brand is missing"))
	}
	return probe, nil
}

func isM4AProbe(probe m4aProbe) bool {
	if !strings.Contains(probe.Format.FormatName, "mp4") && !strings.Contains(probe.Format.FormatName, "m4a") {
		return false
	}
	brands := probe.Format.Tags["major_brand"] + probe.Format.Tags["compatible_brands"]
	return strings.Contains(strings.ToUpper(brands), "M4A")
}

func validateM4AStreams(probe m4aProbe) (m4aProbeStream, m4aProbeStream, error) {
	var audioStreams []m4aProbeStream
	var artworkStreams []m4aProbeStream
	for _, stream := range probe.Streams {
		if stream.CodecType == "audio" {
			audioStreams = append(audioStreams, stream)
		}
		if stream.CodecType == "video" && stream.Disposition["attached_pic"] == 1 {
			artworkStreams = append(artworkStreams, stream)
		}
	}
	if len(audioStreams) != 1 {
		return m4aProbeStream{}, m4aProbeStream{}, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "codec", errors.New("exactly one M4A audio stream is required"))
	}
	if audioStreams[0].CodecName != "aac" && audioStreams[0].CodecName != "alac" {
		return m4aProbeStream{}, m4aProbeStream{}, inspectionError(INSPECTION_ERROR_UNSUPPORTED_FORMAT, "codec", fmt.Errorf("unsupported M4A codec %q", audioStreams[0].CodecName))
	}
	if len(artworkStreams) == 0 {
		return m4aProbeStream{}, m4aProbeStream{}, inspectionError(INSPECTION_ERROR_MISSING_ARTWORK, "artwork", errors.New("embedded front cover is required"))
	}
	if len(artworkStreams) != 1 {
		return m4aProbeStream{}, m4aProbeStream{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("multiple front covers are ambiguous"))
	}
	return audioStreams[0], artworkStreams[0], nil
}

func inspectM4AMetadata(rawTags map[string]string) (NormalizedMediaMetadata, error) {
	tags := make(map[string][]string, len(rawTags))
	for key, value := range rawTags {
		tags[m4aMetadataKey(key)] = []string{value}
	}
	if genres := splitM4AGenres(m4aTagValue(rawTags, "genre")); len(genres) > 0 {
		tags["GENRE"] = genres
	}
	if artists := m4aStructuredCredits(rawTags, "ARTISTS"); len(artists) > 0 {
		tags["ARTIST"] = artists
	}
	if albumArtists := m4aStructuredCredits(rawTags, "ALBUMARTISTS"); len(albumArtists) > 0 {
		tags["ALBUMARTIST"] = albumArtists
	}
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
		Title: names.Title, Artists: names.Artists, AlbumArtists: names.AlbumArtists, Album: names.Album,
		TrackPosition: trackPosition, DiscPosition: discPosition, HasDiscNumber: len(tags["DISCNUMBER"]) > 0,
		Genres: names.Genres, Year: year, ReplayGain: readReplayGainStringMetadata(rawTags),
	}, nil
}

func splitM4AGenres(value string) []string {
	return strings.FieldsFunc(value, func(separator rune) bool {
		return separator == ';' || separator == '/' || separator == '|'
	})
}

func m4aStructuredCredits(tags map[string]string, key string) []string {
	value := m4aTagValue(tags, key)
	if value == "" {
		return nil
	}
	return strings.Split(value, ";")
}

func m4aTagValue(tags map[string]string, key string) string {
	for tagKey, value := range tags {
		if strings.EqualFold(tagKey, key) {
			return value
		}
	}
	return ""
}

func m4aMetadataKey(key string) string {
	switch strings.ToUpper(strings.TrimSpace(key)) {
	case "ALBUM_ARTIST":
		return "ALBUMARTIST"
	case "TRACK":
		return "TRACKNUMBER"
	case "DISC":
		return "DISCNUMBER"
	default:
		return strings.ToUpper(strings.TrimSpace(key))
	}
}

func inspectM4AArtwork(ctx context.Context, path string, stream m4aProbeStream) (AlbumArtwork, error) {
	mediaType := map[string]string{"mjpeg": "image/jpeg", "png": "image/png", "webp": "image/webp"}[stream.CodecName]
	if mediaType == "" {
		return AlbumArtwork{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", fmt.Errorf("unsupported embedded artwork codec %q", stream.CodecName))
	}
	var output boundedBuffer
	output.limit = MAX_ARTWORK_SIZE_BYTES + 1
	command := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-xerror", "-nostdin", "-i", path, "-map", fmt.Sprintf("0:%d", stream.Index), "-frames:v", "1", "-c", "copy", "-f", "image2pipe", "pipe:1")
	command.Stdout = &output
	command.Stderr = newBoundedBuffer(FFMPEG_ERROR_OUTPUT_LIMIT_BYTES)
	err := command.Run()
	if ctx.Err() != nil {
		return AlbumArtwork{}, inspectionCancellationError(ctx)
	}
	if err != nil {
		return AlbumArtwork{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", fmt.Errorf("extract embedded artwork: %w", err))
	}
	return validateArtwork(&flacmeta.Picture{MIME: mediaType, Data: output.Bytes()})
}

func inspectM4AAudio(ctx context.Context, path string, stream m4aProbeStream, reportProgress InspectionProgressReporter) (TechnicalAudioProperties, error) {
	audio, err := buildM4AAudioProperties(stream)
	if err != nil {
		return TechnicalAudioProperties{}, err
	}
	if err := decodeM4AToEOF(ctx, path, stream.Index, audio, reportProgress); err != nil {
		return TechnicalAudioProperties{}, err
	}
	return audio, nil
}

func buildM4AAudioProperties(stream m4aProbeStream) (TechnicalAudioProperties, error) {
	sampleRate, sampleRateErr := strconv.Atoi(stream.SampleRate)
	bitrateBps, bitrateErr := strconv.ParseInt(stream.BitRate, 10, 64)
	durationSeconds, durationErr := strconv.ParseFloat(stream.Duration, 64)
	durationMs := int(durationSeconds * MILLISECONDS_PER_SECOND)
	if sampleRateErr != nil || bitrateErr != nil || durationErr != nil || sampleRate <= 0 || stream.Channels <= 0 || bitrateBps <= 0 || durationMs <= 0 {
		return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("M4A stream has invalid technical properties"))
	}
	bitDepth := 0
	if stream.CodecName == "alac" {
		bitDepth, _ = strconv.Atoi(stream.BitsPerRawSample)
		if bitDepth <= 0 {
			return TechnicalAudioProperties{}, inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", errors.New("ALAC bit depth is missing"))
		}
	}
	return TechnicalAudioProperties{Format: "m4a", Container: "m4a", Codec: stream.CodecName, DurationMs: durationMs, SampleRateHz: sampleRate, ChannelCount: stream.Channels, BitDepth: bitDepth, BitrateKbps: int((bitrateBps + 500) / 1000)}, nil
}

func decodeM4AToEOF(ctx context.Context, path string, streamIndex int, audio TechnicalAudioProperties, reportProgress InspectionProgressReporter) error {
	command := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-xerror", "-nostdin", "-i", path, "-map", fmt.Sprintf("0:%d", streamIndex), "-f", "null", "-", "-progress", "pipe:1", "-nostats")
	progress, err := command.StdoutPipe()
	if err != nil {
		return inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", err)
	}
	command.Stderr = newBoundedBuffer(FFMPEG_ERROR_OUTPUT_LIMIT_BYTES)
	if err := command.Start(); err != nil {
		return inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", err)
	}
	reportErr := readM4ADecodeProgress(progress, audio, reportProgress)
	if reportErr != nil {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return inspectionCancellationError(ctx)
	}
	if reportErr != nil {
		return inspectionProgressError(reportErr)
	}
	if waitErr != nil {
		return inspectionError(INSPECTION_ERROR_AUDIO_DECODE, "audio", fmt.Errorf("decode M4A stream: %w", waitErr))
	}
	return reportDecodedProgress(reportProgress, uint64(audio.DurationMs*audio.SampleRateHz/MILLISECONDS_PER_SECOND), uint64(audio.DurationMs*audio.SampleRateHz/MILLISECONDS_PER_SECOND), 0, 0, true)
}

func readM4ADecodeProgress(reader io.Reader, audio TechnicalAudioProperties, reportProgress InspectionProgressReporter) error {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok || key != "out_time_us" {
			continue
		}
		microseconds, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			continue
		}
		decodedSamples := microseconds * uint64(audio.SampleRateHz) / 1_000_000
		totalSamples := uint64(audio.DurationMs * audio.SampleRateHz / MILLISECONDS_PER_SECOND)
		if err := reportDecodedProgress(reportProgress, decodedSamples, totalSamples, 0, 0, false); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type boundedBuffer struct {
	buffer      bytes.Buffer
	limit       int
	isTruncated bool
}

func newBoundedBuffer(limit int) *boundedBuffer {
	return &boundedBuffer{limit: limit}
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	written := len(data)
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining <= 0 {
		buffer.isTruncated = true
		return written, nil
	}
	if len(data) > remaining {
		buffer.isTruncated = true
		data = data[:remaining]
	}
	_, _ = buffer.buffer.Write(data)
	return written, nil
}

func (buffer *boundedBuffer) Bytes() []byte {
	return buffer.buffer.Bytes()
}

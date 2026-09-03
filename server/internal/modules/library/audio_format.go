package library

func estimateBitrateKbps(sizeBytes int64, durationMs int) int {
	if sizeBytes <= 0 || durationMs <= 0 {
		return 0
	}
	kbps := int((sizeBytes * 8) / int64(durationMs))
	if kbps <= 0 {
		return 0
	}
	return kbps
}

func enrichTrackBitrate(track *Track) {
	if track.BitDepth > 0 || track.SampleRateHz > 0 {
		return
	}
	if kbps := estimateBitrateKbps(track.SizeBytes, track.DurationMs); kbps > 0 {
		track.BitrateKbps = kbps
	}
}

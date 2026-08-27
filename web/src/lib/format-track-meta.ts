import type { Track } from "@repo/api-client";

function formatSampleRate(hz: number): string {
	if (hz % 1000 === 0) {
		return `${hz / 1000} kHz`;
	}
	return `${(hz / 1000).toFixed(1)} kHz`;
}

export function formatTrackMeta(track: Track): string | null {
	const parts: string[] = [];

	if (track.genre) {
		parts.push(track.genre);
	}

	if (track.format) {
		parts.push(track.format.toUpperCase());
	}

	if (track.bitDepth && track.bitDepth > 0) {
		parts.push(`${track.bitDepth}-bit`);
	}

	if (track.sampleRateHz && track.sampleRateHz > 0) {
		parts.push(formatSampleRate(track.sampleRateHz));
	} else if (track.bitrateKbps && track.bitrateKbps > 0) {
		parts.push(`${track.bitrateKbps} kbps`);
	}

	return parts.length > 0 ? parts.join(" · ") : null;
}

export function formatReplayGainAvailability(
	gainDb?: number | null,
	peak?: number | null,
): string {
	const details: string[] = [];
	if (gainDb != null) {
		details.push(`Gain ${gainDb.toFixed(2)} dB`);
	}
	if (peak != null) {
		details.push(`Peak ${peak.toFixed(6)}`);
	}
	if (details.length === 0) {
		return "Unavailable";
	}
	return `Available · ${details.join(" · ")}`;
}

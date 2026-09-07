import { useEffect, useState } from "react";

export type TrackWaveformResult =
	| { trackId: string; peakCount: number; peaks: number[] }
	| { status: "pending"; retryAfterSeconds: number };

export type TrackWaveformLoader = (
	trackId: string,
) => Promise<TrackWaveformResult>;

const waveformCache = new Map<string, number[]>();
const MAX_PENDING_POLLS = 30;

/** Test hook: forget every cached waveform. */
export function clearTrackWaveformCache() {
	waveformCache.clear();
}

/**
 * Peaks for the current Track, or null while unknown. The server generates
 * peaks lazily and answers "pending" meanwhile, so the hook polls at the
 * server's suggested interval; results are cached per track for the session.
 */
export function useTrackWaveform({
	trackId,
	enabled,
	load,
}: {
	trackId: string | null;
	enabled: boolean;
	load: TrackWaveformLoader | null;
}): number[] | null {
	const [peaks, setPeaks] = useState<number[] | null>(null);

	useEffect(() => {
		if (!enabled || !trackId || !load) {
			setPeaks(null);
			return undefined;
		}
		const cached = waveformCache.get(trackId);
		if (cached) {
			setPeaks(cached);
			return undefined;
		}
		setPeaks(null);
		let cancelled = false;
		let timer: number | null = null;
		let polls = 0;
		const request = () => {
			load(trackId)
				.then((result) => {
					if (cancelled) return;
					if ("status" in result) {
						polls += 1;
						if (polls > MAX_PENDING_POLLS) return;
						timer = window.setTimeout(
							request,
							Math.max(1, result.retryAfterSeconds) * 1000,
						);
						return;
					}
					waveformCache.set(trackId, result.peaks);
					setPeaks(result.peaks);
				})
				.catch(() => {
					// No waveform (older server, missing ffmpeg, missing file): the
					// seek bar simply stays plain.
				});
		};
		request();
		return () => {
			cancelled = true;
			if (timer !== null) window.clearTimeout(timer);
		};
	}, [enabled, trackId, load]);

	return peaks;
}

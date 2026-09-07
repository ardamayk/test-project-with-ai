import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	clearTrackWaveformCache,
	type TrackWaveformLoader,
	useTrackWaveform,
} from "./use-track-waveform";

function Harness({
	trackId,
	enabled,
	load,
}: {
	trackId: string | null;
	enabled: boolean;
	load: TrackWaveformLoader | null;
}) {
	const peaks = useTrackWaveform({ trackId, enabled, load });
	return <span data-testid="peaks">{peaks ? peaks.join(",") : "none"}</span>;
}

describe("useTrackWaveform", () => {
	beforeEach(clearTrackWaveformCache);
	afterEach(() => {
		cleanup();
		vi.useRealTimers();
	});

	it("polls while pending, then caches the peaks per track", async () => {
		vi.useFakeTimers();
		const load = vi
			.fn<TrackWaveformLoader>()
			.mockResolvedValueOnce({ status: "pending", retryAfterSeconds: 2 })
			.mockResolvedValueOnce({
				trackId: "t1",
				peakCount: 3,
				peaks: [0, 128, 255],
			});
		const view = render(<Harness trackId="t1" enabled load={load} />);
		await act(async () => {});
		expect(screen.getByTestId("peaks").textContent).toBe("none");

		await act(async () => {
			vi.advanceTimersByTime(2000);
		});
		await act(async () => {});
		expect(screen.getByTestId("peaks").textContent).toBe("0,128,255");
		expect(load).toHaveBeenCalledTimes(2);

		view.rerender(<Harness trackId="t1" enabled load={load} />);
		expect(load).toHaveBeenCalledTimes(2);
	});

	it("stays empty when disabled or when the server has no waveform", async () => {
		const failing = vi.fn<TrackWaveformLoader>(async () => {
			throw new Error("503");
		});
		render(<Harness trackId="t2" enabled load={failing} />);
		await act(async () => {});
		expect(screen.getByTestId("peaks").textContent).toBe("none");

		const load = vi.fn<TrackWaveformLoader>();
		render(<Harness trackId="t3" enabled={false} load={load} />);
		expect(load).not.toHaveBeenCalled();
	});
});

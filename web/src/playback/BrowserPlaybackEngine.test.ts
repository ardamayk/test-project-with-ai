import { playbackEngineContract } from "@repo/ui/playback/testing";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	BrowserPlaybackEngine,
	type BrowserPlaybackMedia,
} from "./BrowserPlaybackEngine";

class MemoryMedia extends EventTarget implements BrowserPlaybackMedia {
	currentTime = 0;
	duration = 0;
	paused = true;
	src = "";
	volume = 1;
	canPlayType = vi.fn(() => "");
	play = vi.fn(async () => {
		this.paused = false;
		this.dispatchEvent(new Event("play"));
		this.dispatchEvent(new Event("playing"));
	});
	pause = vi.fn(() => {
		this.paused = true;
		this.dispatchEvent(new Event("pause"));
	});
	load = vi.fn();
	removeAttribute = vi.fn((name: string) => {
		if (name === "src") this.src = "";
	});
}

let media: MemoryMedia;

beforeEach(() => {
	media = new MemoryMedia();
});

afterEach(() => {
	vi.useRealTimers();
});

playbackEngineContract("browser", () => {
	const engine = new BrowserPlaybackEngine({ createMedia: () => media });
	return {
		engine,
		finish: () => media.dispatchEvent(new Event("ended")),
	};
});

describe("BrowserPlaybackEngine", () => {
	it("reconnects a started Radio Station after its stream ends", async () => {
		vi.useFakeTimers();
		const engine = new BrowserPlaybackEngine({ createMedia: () => media });
		const source = {
			type: "radio-station" as const,
			station: {
				id: "station-reconnect",
				name: "Reconnect Radio",
				streamUrl: "https://example.com/live.mp3",
				tags: [],
				source: "manual" as const,
				isFavorite: false,
				position: 0,
			},
			playbackUrl: "/api/v1/radio/stations/station-reconnect/stream",
			sourceUrl: "https://example.com/live.mp3",
		};

		await engine.play(source);
		media.paused = true;
		media.dispatchEvent(new Event("ended"));

		expect(engine.getState().status).toBe("reconnecting");
		await vi.advanceTimersByTimeAsync(999);
		expect(media.play).toHaveBeenCalledOnce();
		await vi.advanceTimersByTimeAsync(1);
		expect(media.play).toHaveBeenCalledTimes(2);
		expect(engine.getState().status).toBe("playing");
		engine.destroy();
	});

	it("reconnects a started Catalog Preview after a media error", async () => {
		vi.useFakeTimers();
		const engine = new BrowserPlaybackEngine({ createMedia: () => media });
		await engine.play({
			type: "catalog-preview",
			result: {
				stationUuid: "catalog-reconnect",
				name: "Reconnect Preview",
				streamUrl: "https://example.com/preview.mp3",
				tags: [],
			},
			playbackUrl: "/api/v1/radio/preview/catalog-reconnect/stream",
			sourceUrl: "https://example.com/preview.mp3",
		});

		media.paused = true;
		media.dispatchEvent(new Event("error"));

		expect(engine.getState().status).toBe("reconnecting");
		await vi.advanceTimersByTimeAsync(1000);
		expect(media.play).toHaveBeenCalledTimes(2);
		engine.destroy();
	});

	it("keeps an initial live media failure terminal before playback is stable", async () => {
		vi.useFakeTimers();
		media.play.mockImplementationOnce(async () => {
			media.paused = false;
			media.dispatchEvent(new Event("play"));
		});
		const engine = new BrowserPlaybackEngine({ createMedia: () => media });
		await engine.play({
			type: "radio-station",
			station: {
				id: "station-initial-failure",
				name: "Initial Failure",
				streamUrl: "https://example.com/initial-failure.mp3",
				tags: [],
				source: "manual",
				isFavorite: false,
				position: 0,
			},
			playbackUrl: "/api/v1/radio/stations/station-initial-failure/stream",
			sourceUrl: "https://example.com/initial-failure.mp3",
		});

		media.dispatchEvent(new Event("error"));
		await vi.advanceTimersByTimeAsync(15000);

		expect(engine.getState().status).toBe("error");
		expect(media.play).toHaveBeenCalledOnce();
		engine.destroy();
	});

	it("reconnects when a started live stream remains stalled", async () => {
		vi.useFakeTimers();
		const engine = new BrowserPlaybackEngine({ createMedia: () => media });
		await engine.play({
			type: "catalog-preview",
			result: {
				stationUuid: "catalog-stalled",
				name: "Stalled Preview",
				streamUrl: "https://example.com/stalled.mp3",
				tags: [],
			},
			playbackUrl: "/api/v1/radio/preview/catalog-stalled/stream",
			sourceUrl: "https://example.com/stalled.mp3",
		});

		media.dispatchEvent(new Event("stalled"));
		await vi.advanceTimersByTimeAsync(9999);
		expect(engine.getState().status).toBe("playing");
		await vi.advanceTimersByTimeAsync(1);
		expect(engine.getState().status).toBe("reconnecting");
		await vi.advanceTimersByTimeAsync(1000);
		expect(media.play).toHaveBeenCalledTimes(2);
		engine.destroy();
	});

	it("cancels the stall watchdog when live playback advances", async () => {
		vi.useFakeTimers();
		const engine = new BrowserPlaybackEngine({ createMedia: () => media });
		await engine.play({
			type: "catalog-preview",
			result: {
				stationUuid: "catalog-recovered-stall",
				name: "Recovered Stall",
				streamUrl: "https://example.com/recovered-stall.mp3",
				tags: [],
			},
			playbackUrl: "/api/v1/radio/preview/catalog-recovered-stall/stream",
			sourceUrl: "https://example.com/recovered-stall.mp3",
		});

		media.dispatchEvent(new Event("stalled"));
		await vi.advanceTimersByTimeAsync(5000);
		media.currentTime = 5;
		media.dispatchEvent(new Event("timeupdate"));
		await vi.advanceTimersByTimeAsync(10000);

		expect(engine.getState().status).toBe("playing");
		expect(media.play).toHaveBeenCalledOnce();
		engine.destroy();
	});

	it("resets live reconnect backoff after stable playback", async () => {
		vi.useFakeTimers();
		const engine = new BrowserPlaybackEngine({ createMedia: () => media });
		await engine.play({
			type: "catalog-preview",
			result: {
				stationUuid: "catalog-stable",
				name: "Stable Preview",
				streamUrl: "https://example.com/stable.mp3",
				tags: [],
			},
			playbackUrl: "/api/v1/radio/preview/catalog-stable/stream",
			sourceUrl: "https://example.com/stable.mp3",
		});

		media.paused = true;
		media.dispatchEvent(new Event("ended"));
		await vi.advanceTimersByTimeAsync(1000);
		expect(media.play).toHaveBeenCalledTimes(2);

		await vi.advanceTimersByTimeAsync(30000);
		media.paused = true;
		media.dispatchEvent(new Event("ended"));
		await vi.advanceTimersByTimeAsync(1000);

		expect(media.play).toHaveBeenCalledTimes(3);
		engine.destroy();
	});

	it("lets the user pause an active reconnect attempt", async () => {
		vi.useFakeTimers();
		const engine = new BrowserPlaybackEngine({ createMedia: () => media });
		await engine.play({
			type: "catalog-preview",
			result: {
				stationUuid: "catalog-pause-reconnect",
				name: "Pause Reconnect",
				streamUrl: "https://example.com/pause.mp3",
				tags: [],
			},
			playbackUrl: "/api/v1/radio/preview/catalog-pause-reconnect/stream",
			sourceUrl: "https://example.com/pause.mp3",
		});
		media.paused = true;
		media.dispatchEvent(new Event("ended"));

		engine.togglePlay();
		await vi.advanceTimersByTimeAsync(15000);

		expect(engine.getState().status).toBe("paused");
		expect(media.play).toHaveBeenCalledOnce();
		engine.destroy();
	});

	it("does not resume when the user pauses an in-flight reconnect", async () => {
		vi.useFakeTimers();
		const engine = new BrowserPlaybackEngine({ createMedia: () => media });
		await engine.play({
			type: "catalog-preview",
			result: {
				stationUuid: "catalog-in-flight-pause",
				name: "In-flight Pause",
				streamUrl: "https://example.com/in-flight.mp3",
				tags: [],
			},
			playbackUrl: "/api/v1/radio/preview/catalog-in-flight-pause/stream",
			sourceUrl: "https://example.com/in-flight.mp3",
		});
		let resolveReconnect: () => void = () => undefined;
		media.play.mockImplementationOnce(
			() =>
				new Promise<void>((resolve) => {
					resolveReconnect = resolve;
				}),
		);
		media.dispatchEvent(new Event("ended"));

		await vi.advanceTimersByTimeAsync(1000);
		engine.pause();
		resolveReconnect();
		await Promise.resolve();
		await vi.advanceTimersByTimeAsync(30000);

		expect(engine.getState().status).toBe("paused");
		expect(media.play).toHaveBeenCalledTimes(2);
		engine.destroy();
	});

	it("caps persistent live reconnect backoff at fifteen seconds", async () => {
		vi.useFakeTimers();
		const engine = new BrowserPlaybackEngine({ createMedia: () => media });
		await engine.play({
			type: "catalog-preview",
			result: {
				stationUuid: "catalog-backoff",
				name: "Backoff Preview",
				streamUrl: "https://example.com/backoff.mp3",
				tags: [],
			},
			playbackUrl: "/api/v1/radio/preview/catalog-backoff/stream",
			sourceUrl: "https://example.com/backoff.mp3",
		});
		media.play.mockRejectedValue(new Error("stream unavailable"));
		media.paused = true;
		media.dispatchEvent(new Event("ended"));

		for (const delay of [1000, 2000, 4000, 8000, 15000, 15000]) {
			await vi.advanceTimersByTimeAsync(delay);
		}

		expect(media.play).toHaveBeenCalledTimes(7);
		expect(engine.getState().status).toBe("reconnecting");
		engine.destroy();
	});

	it("publishes Previous and Next through the browser queue fallback seam", () => {
		const engine = new BrowserPlaybackEngine({ createMedia: () => media });
		const listener = vi.fn();
		engine.subscribeNavigation(listener);

		engine.previous();
		engine.next();

		expect(listener).toHaveBeenNthCalledWith(1, "previous");
		expect(listener).toHaveBeenNthCalledWith(2, "next");
		engine.destroy();
	});

	it("uses hls.js for proxied HLS Radio Stations", async () => {
		const hls = {
			attachMedia: vi.fn(),
			destroy: vi.fn(),
			loadSource: vi.fn(),
		};
		const engine = new BrowserPlaybackEngine({
			createMedia: () => media,
			hls: {
				isSupported: () => true,
				create: () => hls,
			},
		});

		await engine.play({
			type: "radio-station",
			station: {
				id: "station-hls",
				name: "HLS Station",
				streamUrl: "https://example.com/live/index.m3u8",
				tags: [],
				source: "manual",
				isFavorite: false,
				position: 0,
			},
			playbackUrl: "/api/v1/radio/stations/station-hls/stream",
			sourceUrl: "https://example.com/live/index.m3u8",
		});

		expect(hls.loadSource).toHaveBeenCalledWith(
			"/api/v1/radio/stations/station-hls/stream",
		);
		expect(hls.attachMedia).toHaveBeenCalledWith(media);
		expect(media.src).toBe("");
		engine.destroy();
	});

	it("reconnects a started HLS stream after a fatal HLS error", async () => {
		vi.useFakeTimers();
		let reportFatalError: () => void = () => undefined;
		const hls = {
			attachMedia: vi.fn(),
			destroy: vi.fn(),
			loadSource: vi.fn(),
			onFatalError: vi.fn((listener: () => void) => {
				reportFatalError = listener;
			}),
		};
		const engine = new BrowserPlaybackEngine({
			createMedia: () => media,
			hls: {
				isSupported: () => true,
				create: () => hls,
			},
		});
		await engine.play({
			type: "radio-station",
			station: {
				id: "station-fatal-hls",
				name: "Fatal HLS",
				streamUrl: "https://example.com/live/index.m3u8",
				tags: [],
				source: "manual",
				isFavorite: false,
				position: 0,
			},
			playbackUrl: "/api/v1/radio/stations/station-fatal-hls/stream",
			sourceUrl: "https://example.com/live/index.m3u8",
		});

		reportFatalError();

		expect(engine.getState().status).toBe("reconnecting");
		await vi.advanceTimersByTimeAsync(1000);
		expect(media.play).toHaveBeenCalledTimes(2);
		engine.destroy();
	});

	it("publishes playback errors without exposing media errors", async () => {
		media.play.mockRejectedValueOnce(new Error("codec unavailable"));
		const engine = new BrowserPlaybackEngine({ createMedia: () => media });

		await expect(
			engine.play({
				type: "catalog-preview",
				result: {
					stationUuid: "catalog-1",
					name: "Catalog",
					streamUrl: "https://example.com/live",
					tags: [],
				},
				playbackUrl: "/api/v1/radio/preview/catalog-1/stream",
				sourceUrl: "https://example.com/live",
			}),
		).rejects.toThrow("codec unavailable");
		expect(engine.getState()).toMatchObject({
			status: "error",
			error: {
				code: "playback-failed",
				message: "codec unavailable",
			},
		});
		engine.destroy();
	});
});

import { playbackEngineContract } from "@repo/ui/playback/testing";
import { beforeEach, describe, expect, it, vi } from "vitest";
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
	});
	pause = vi.fn(() => {
		this.paused = true;
		this.dispatchEvent(new Event("pause"));
	});
	removeAttribute = vi.fn((name: string) => {
		if (name === "src") this.src = "";
	});
}

let media: MemoryMedia;

beforeEach(() => {
	media = new MemoryMedia();
});

playbackEngineContract("browser", () => {
	const engine = new BrowserPlaybackEngine({ createMedia: () => media });
	return {
		engine,
		finish: () => media.dispatchEvent(new Event("ended")),
	};
});

describe("BrowserPlaybackEngine", () => {
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
				playbackUrl: "/api/v1/radio/catalog/catalog-1/stream",
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

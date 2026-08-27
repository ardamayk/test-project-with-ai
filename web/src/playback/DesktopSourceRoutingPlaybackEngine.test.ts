import type { PlaybackSessionState, PlaybackSource } from "@repo/ui";
import { InMemoryPlaybackEngine } from "@repo/ui/playback/testing";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
	BrowserPlaybackEngine,
	type BrowserPlaybackMedia,
} from "./BrowserPlaybackEngine";
import { DesktopSourceRoutingPlaybackEngine } from "./DesktopSourceRoutingPlaybackEngine";

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

const trackSource: PlaybackSource = {
	type: "track",
	track: {
		id: "track-1",
		title: "Track 1",
		artistName: "Artist",
		albumId: "album-1",
		durationMs: 120000,
		format: "flac",
	},
	playbackUrl: "/api/v1/tracks/track-1/stream",
};

const radioSource: PlaybackSource = {
	type: "radio-station",
	station: {
		id: "station-1",
		name: "Station 1",
		streamUrl: "https://radio.example/live",
		tags: [],
		source: "manual",
		isFavorite: false,
		position: 0,
	},
	playbackUrl: "/api/v1/radio/stations/station-1/stream",
	sourceUrl: "https://radio.example/live",
};

const catalogSource: PlaybackSource = {
	type: "catalog-preview",
	result: {
		stationUuid: "catalog-1",
		name: "Catalog 1",
		streamUrl: "https://catalog.example/live",
		tags: [],
	},
	playbackUrl: "/api/v1/radio/catalog/catalog-1/stream",
	sourceUrl: "https://catalog.example/live",
};

describe("DesktopSourceRoutingPlaybackEngine", () => {
	afterEach(() => vi.unstubAllGlobals());

	it("plays library Tracks natively without creating HTML audio", async () => {
		const nativeEngine = new InMemoryPlaybackEngine();
		const audioConstructor = vi.fn();
		vi.stubGlobal("Audio", audioConstructor);
		const engine = new DesktopSourceRoutingPlaybackEngine(
			nativeEngine,
			() => new BrowserPlaybackEngine(),
		);

		await engine.play(trackSource);
		engine.seek(17);
		engine.pause();

		expect(engine.getState()).toMatchObject({
			source: trackSource,
			status: "paused",
			currentTime: 17,
		});
		expect(nativeEngine.getState().source).toEqual(trackSource);
		expect(audioConstructor).not.toHaveBeenCalled();
		engine.destroy();
	});

	it("keeps saved Radio playback and controls in the browser engine", async () => {
		const nativeEngine = new InMemoryPlaybackEngine();
		const media = new MemoryMedia();
		const engine = new DesktopSourceRoutingPlaybackEngine(
			nativeEngine,
			() => new BrowserPlaybackEngine({ createMedia: () => media }),
		);
		const observedStates: PlaybackSessionState[] = [];
		engine.subscribe((state) => observedStates.push(state));

		await engine.play(radioSource);
		media.currentTime = 23;
		media.dispatchEvent(new Event("timeupdate"));
		engine.pause();

		expect(nativeEngine.getState().source).toBeNull();
		expect(media.src).toBe(radioSource.playbackUrl);
		expect(media.play).toHaveBeenCalled();
		expect(media.pause).toHaveBeenCalled();
		expect(engine.getState()).toMatchObject({
			source: radioSource,
			status: "paused",
			currentTime: 23,
		});
		expect(observedStates.some((state) => state.currentTime === 23)).toBe(true);
		engine.destroy();
	});

	it("keeps Catalog Preview playback in the browser engine", async () => {
		const nativeEngine = new InMemoryPlaybackEngine();
		const media = new MemoryMedia();
		const engine = new DesktopSourceRoutingPlaybackEngine(
			nativeEngine,
			() => new BrowserPlaybackEngine({ createMedia: () => media }),
		);

		await engine.play(catalogSource);

		expect(nativeEngine.getState().source).toBeNull();
		expect(media.src).toBe(catalogSource.playbackUrl);
		expect(engine.getState().source).toEqual(catalogSource);
		engine.destroy();
	});
});

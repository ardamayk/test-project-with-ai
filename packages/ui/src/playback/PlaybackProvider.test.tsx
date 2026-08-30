import {
	ApiError,
	type QueueEvent,
	type RadioSearchResult,
	type RadioStation,
} from "@repo/api-client";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
	type PlaybackApi,
	PlaybackProvider,
	usePlayback,
} from "./PlaybackProvider";
import { InMemoryPlaybackEngine } from "./testing/InMemoryPlaybackEngine";

const track = {
	id: "track-1",
	title: "Track 1",
	artistName: "Artist",
	albumId: "album-1",
	durationMs: 120000,
	format: "flac",
};

const track2 = {
	...track,
	id: "track-2",
	title: "Track 2",
	durationMs: 180000,
};

const track3 = {
	...track,
	id: "track-3",
	title: "Track 3",
	durationMs: 240000,
};

const radioStation: RadioStation = {
	id: "station-1",
	name: "Station 1",
	streamUrl: "https://example.com/live",
	tags: [],
	source: "manual",
	isFavorite: false,
	position: 0,
};

const catalogResult: RadioSearchResult = {
	stationUuid: "catalog-1",
	name: "Catalog 1",
	streamUrl: "https://example.com/preview",
	tags: [],
};

const queueItems = [
	{ id: "item-1", trackId: track.id, position: 0, track },
	{ id: "item-2", trackId: track2.id, position: 1, track: track2 },
];

function createApi(items = queueItems): PlaybackApi {
	return {
		getQueue: vi.fn(async () => ({ items, revision: "1" })),
		replaceQueue: vi.fn(async (trackIds: string[]) => ({
			items: trackIds.map((trackId, position) => ({
				id: `item-${position + 1}`,
				trackId,
				position,
				track: trackId === track2.id ? track2 : track,
			})),
			revision: "2",
		})),
		reorderQueue: vi.fn(async () => ({ items, revision: "2" })),
		appendQueueItem: vi.fn(async () => ({ items: queueItems, revision: "2" })),
		removeQueueItem: vi.fn(async () => ({ items: [], revision: "2" })),
		subscribeQueueEvents: vi.fn(() => vi.fn()),
		getStreamUrl: (trackId) => `/stream/${trackId}`,
		getAlbumCoverUrl: (albumId) => `/cover/${albumId}`,
		getRadioStationStreamUrl: (stationId) => `/radio/${stationId}`,
		getRadioCatalogPreviewStreamUrl: (stationUuid) =>
			`/radio/preview/${stationUuid}`,
		getRadioNowPlaying: vi.fn(async () => ({ raw: "Artist - Song" })),
		listPlaylists: vi.fn(async () => ({ items: [], total: 0 })),
		getPlaylist: vi.fn(async (playlistId: string) => ({
			id: playlistId,
			name: "Playlist",
			isDefault: false,
			trackCount: 0,
			tracks: [],
		})),
		createPlaylist: vi.fn(async (name: string) => ({
			id: "playlist-1",
			name,
			isDefault: false,
			trackCount: 0,
		})),
		addPlaylistTrack: vi.fn(async () => ({
			id: "playlist-1",
			name: "Playlist",
			isDefault: false,
			trackCount: 1,
			tracks: [track],
		})),
		removePlaylistTrack: vi.fn(async () => ({
			id: "playlist-1",
			name: "Playlist",
			isDefault: false,
			trackCount: 0,
			tracks: [],
		})),
	};
}

function Harness() {
	const playback = usePlayback();
	return (
		<div>
			<span data-testid="queue">
				{playback.queue.map((item) => item.track.id).join(",")}
			</span>
			<span data-testid="track">{playback.currentTrack?.title ?? ""}</span>
			<span data-testid="radio">
				{playback.currentRadioStation?.name ?? ""}
			</span>
			<span data-testid="now-playing">
				{playback.radioNowPlaying?.raw ?? ""}
			</span>
			<span data-testid="playing">{String(playback.isPlaying)}</span>
			<span data-testid="reconnecting">{String(playback.isReconnecting)}</span>
			<span data-testid="volume">{playback.volume}</span>
			<span data-testid="repeat">{playback.repeatMode}</span>
			<span data-testid="queue-conflict">{playback.queueConflict ?? ""}</span>
			<button type="button" onClick={() => void playback.playTrack(track.id)}>
				Track
			</button>
			<button type="button" onClick={() => void playback.playTrack(track2.id)}>
				Track 2
			</button>
			<button type="button" onClick={() => void playback.playQueueIndex(0)}>
				First Queue item
			</button>
			<button
				type="button"
				onClick={() => void playback.playRadioStation(radioStation)}
			>
				Radio
			</button>
			<button
				type="button"
				onClick={() => void playback.playRadioCatalogPreview(catalogResult)}
			>
				Preview
			</button>
			<button type="button" onClick={() => playback.setVolume(0.3)}>
				Volume
			</button>
			<button type="button" onClick={playback.cycleRepeatMode}>
				Repeat
			</button>
			<button type="button" onClick={() => void playback.clearQueue()}>
				Clear
			</button>
			<button
				type="button"
				onClick={() => void playback.queueTracks([track2.id])}
			>
				Append
			</button>
			<button
				type="button"
				onClick={() => void playback.removeFromQueue("item-1")}
			>
				Remove
			</button>
			<button
				type="button"
				onClick={() => void playback.reorderQueue(["item-2", "item-1"])}
			>
				Reorder
			</button>
		</div>
	);
}

function renderPlayback(
	api = createApi(),
	engine: InMemoryPlaybackEngine = new InMemoryPlaybackEngine(),
) {
	render(
		<PlaybackProvider api={api} engine={engine}>
			<Harness />
		</PlaybackProvider>,
	);
	return { api, engine };
}

afterEach(cleanup);

describe("PlaybackProvider", () => {
	it("plays previous and next Queue items from native tray navigation", async () => {
		const { engine } = renderPlayback();
		await act(async () => {});
		await act(async () =>
			screen.getByRole("button", { name: "Track" }).click(),
		);

		await act(async () => engine.navigate("next"));
		expect(engine.getState().source).toMatchObject({
			type: "track",
			track: { id: "track-2" },
		});

		await act(async () => engine.navigate("previous"));
		expect(engine.getState().source).toMatchObject({
			type: "track",
			track: { id: "track-1" },
		});
	});

	it("maps queued Tracks to PlaybackEngine sources", async () => {
		const { engine } = renderPlayback();
		await act(async () =>
			screen.getByRole("button", { name: "Track" }).click(),
		);

		expect(engine.getState().source).toEqual({
			type: "track",
			track,
			playbackUrl: "/stream/track-1",
			queueItemId: "item-1",
		});
		expect(screen.getByTestId("track").textContent).toBe("Track 1");
		expect(screen.getByTestId("playing").textContent).toBe("true");
	});

	it("plays Radio Stations and Catalog Previews without changing Queue", async () => {
		const api = createApi();
		const { engine } = renderPlayback(api);
		await act(async () => {});
		const queueBeforePlayback = screen.getByTestId("queue").textContent;
		await act(async () =>
			screen.getByRole("button", { name: "Track" }).click(),
		);
		await act(async () =>
			screen.getByRole("button", { name: "Radio" }).click(),
		);
		expect(engine.getState().source).toMatchObject({
			type: "radio-station",
			playbackUrl: "/radio/station-1",
			sourceUrl: radioStation.streamUrl,
		});
		expect(screen.getByTestId("track").textContent).toBe("");
		expect(screen.getByTestId("radio").textContent).toBe("Station 1");
		await waitFor(() => {
			expect(screen.getByTestId("now-playing").textContent).toBe(
				"Artist - Song",
			);
		});
		expect(api.getRadioNowPlaying).toHaveBeenCalledWith(radioStation.id);

		await act(async () =>
			screen.getByRole("button", { name: "Preview" }).click(),
		);
		expect(engine.getState().source).toMatchObject({
			type: "catalog-preview",
			playbackUrl: "/radio/preview/catalog-1",
			sourceUrl: catalogResult.streamUrl,
		});
		expect(screen.getByTestId("track").textContent).toBe("");
		expect(screen.getByTestId("radio").textContent).toBe("Catalog 1");
		expect(screen.getByTestId("now-playing").textContent).toBe("");
		expect(api.getRadioNowPlaying).toHaveBeenCalledTimes(1);
		expect(screen.getByTestId("queue").textContent).toBe(queueBeforePlayback);
		expect(api.replaceQueue).not.toHaveBeenCalled();
		expect(api.appendQueueItem).not.toHaveBeenCalled();
		expect(api.removeQueueItem).not.toHaveBeenCalled();
		expect(api.reorderQueue).not.toHaveBeenCalled();

		await act(async () =>
			screen.getByRole("button", { name: "Track" }).click(),
		);
		expect(engine.getState().source).toMatchObject({
			type: "track",
			track: { id: track.id },
		});
		expect(screen.getByTestId("radio").textContent).toBe("");
		expect(screen.getByTestId("queue").textContent).toBe(queueBeforePlayback);
	});

	it("exposes an active reconnecting Radio Station session", async () => {
		const { engine } = renderPlayback();
		await act(async () =>
			screen.getByRole("button", { name: "Radio" }).click(),
		);

		act(() => engine.reconnect());

		expect(screen.getByTestId("playing").textContent).toBe("true");
		expect(screen.getByTestId("reconnecting").textContent).toBe("true");
	});

	it("delegates Playback Session controls to PlaybackEngine", async () => {
		const { engine } = renderPlayback();
		await act(async () =>
			screen.getByRole("button", { name: "Track" }).click(),
		);
		await act(async () => {
			screen.getByRole("button", { name: "Volume" }).click();
			screen.getByRole("button", { name: "Repeat" }).click();
		});

		expect(engine.getState()).toMatchObject({
			volume: 0.3,
			repeatMode: "once",
		});
		expect(screen.getByTestId("volume").textContent).toBe("0.3");
		expect(screen.getByTestId("repeat").textContent).toBe("once");
	});

	it("advances Queue after engine reports Track ended", async () => {
		const { engine } = renderPlayback();
		await act(async () =>
			screen.getByRole("button", { name: "Track" }).click(),
		);
		await act(async () => engine.finish());

		expect(engine.getState().source).toMatchObject({
			type: "track",
			track: { id: "track-2" },
		});
	});

	it("delegates Queue context and end advancement to a native engine", async () => {
		const engine = new InMemoryPlaybackEngine();
		const syncQueueContext = vi.fn(async () => {});
		Object.assign(engine, { syncQueueContext });
		renderPlayback(createApi(), engine);
		await act(async () => {});
		await act(async () =>
			screen.getByRole("button", { name: "Track" }).click(),
		);

		expect(syncQueueContext).toHaveBeenLastCalledWith(
			[
				expect.objectContaining({ queueItemId: "item-1" }),
				expect.objectContaining({ queueItemId: "item-2" }),
			],
			0,
		);
		await act(async () => engine.finish());
		expect(engine.getState().status).toBe("ended");
		expect(engine.getState().source).toMatchObject({
			track: { id: "track-1" },
		});
	});

	it("advances duplicate Tracks by Queue item identity", async () => {
		const duplicateItems = [
			{ id: "item-1", trackId: track.id, position: 0, track },
			{ id: "item-2", trackId: track.id, position: 1, track },
			{ id: "item-3", trackId: track2.id, position: 2, track: track2 },
		];
		const { engine } = renderPlayback(createApi(duplicateItems));
		await act(async () => {});
		await act(async () =>
			screen.getByRole("button", { name: "First Queue item" }).click(),
		);

		await act(async () => engine.finish());
		await act(async () => engine.finish());

		expect(engine.getState()).toMatchObject({
			source: { type: "track", track: { id: "track-2" } },
			status: "playing",
		});
	});

	it("lets current Track finish before stopping after Queue clear", async () => {
		const { engine } = renderPlayback();
		await act(async () =>
			screen.getByRole("button", { name: "Track" }).click(),
		);
		await act(async () =>
			screen.getByRole("button", { name: "Clear" }).click(),
		);

		expect(engine.getState()).toMatchObject({
			source: { type: "track", track: { id: "track-1" } },
			status: "playing",
		});

		await act(async () => engine.finish());
		expect(engine.getState()).toMatchObject({ source: null, status: "idle" });
	});

	it("keeps current audio after remote removal and resolves latest Queue when it ends", async () => {
		const api = createApi();
		vi.mocked(api.getQueue)
			.mockResolvedValueOnce({ items: queueItems, revision: "1" })
			.mockResolvedValueOnce({ items: [queueItems[1]], revision: "2" });
		const { engine } = renderPlayback(api);
		await act(async () => {});
		await act(async () =>
			screen.getByRole("button", { name: "Track" }).click(),
		);

		await notifyQueueEvent(api, {
			revision: "2",
			sequence: "2",
			invalidates: ["queue"],
		});
		expect(engine.getState()).toMatchObject({
			source: { type: "track", track: { id: "track-1" } },
			status: "playing",
		});

		await act(async () => engine.finish());
		expect(engine.getState()).toMatchObject({
			source: { type: "track", track: { id: "track-2" } },
			status: "playing",
		});
	});

	it("advances from a remotely removed non-head Track to its former successor", async () => {
		const threeItemQueue = [
			queueItems[0],
			queueItems[1],
			{ id: "item-3", trackId: track3.id, position: 2, track: track3 },
		];
		const api = createApi(threeItemQueue);
		vi.mocked(api.getQueue)
			.mockResolvedValueOnce({ items: threeItemQueue, revision: "1" })
			.mockResolvedValueOnce({
				items: [threeItemQueue[0], threeItemQueue[2]],
				revision: "2",
			});
		const { engine } = renderPlayback(api);
		await act(async () => {});
		await act(async () =>
			screen.getByRole("button", { name: "Track 2" }).click(),
		);

		await notifyQueueEvent(api, {
			revision: "2",
			sequence: "2",
			invalidates: ["queue"],
		});
		await act(async () => engine.finish());

		expect(engine.getState()).toMatchObject({
			source: { type: "track", track: { id: "track-3" } },
			status: "playing",
		});
	});

	it("keeps current audio after remote Queue clear and stops when it ends", async () => {
		const api = createApi();
		vi.mocked(api.getQueue)
			.mockResolvedValueOnce({ items: queueItems, revision: "1" })
			.mockResolvedValueOnce({ items: [], revision: "2" });
		const { engine } = renderPlayback(api);
		await act(async () => {});
		await act(async () =>
			screen.getByRole("button", { name: "Track" }).click(),
		);

		await notifyQueueEvent(api, {
			revision: "2",
			sequence: "2",
			invalidates: ["queue"],
		});
		expect(engine.getState()).toMatchObject({
			source: { type: "track", track: { id: "track-1" } },
			status: "playing",
		});

		await act(async () => engine.finish());
		expect(engine.getState()).toMatchObject({ source: null, status: "idle" });
	});

	it.each([
		["once", 1],
		["loop", 2],
	] as const)("remote Queue clear overrides %s repeat after current Track", async (_repeatMode, repeatClicks) => {
		const api = createApi();
		vi.mocked(api.getQueue)
			.mockResolvedValueOnce({ items: queueItems, revision: "1" })
			.mockResolvedValueOnce({ items: [], revision: "2" });
		const { engine } = renderPlayback(api);
		await act(async () => {});
		await act(async () =>
			screen.getByRole("button", { name: "Track" }).click(),
		);
		await act(async () => {
			for (let clickIndex = 0; clickIndex < repeatClicks; clickIndex += 1) {
				screen.getByRole("button", { name: "Repeat" }).click();
			}
		});

		await notifyQueueEvent(api, {
			revision: "2",
			sequence: "2",
			invalidates: ["queue"],
		});
		expect(engine.getState()).toMatchObject({
			source: { type: "track", track: { id: "track-1" } },
			status: "playing",
			repeatMode: "off",
		});
		await act(async () => engine.finish());

		expect(engine.getState()).toMatchObject({ source: null, status: "idle" });
	});

	it("never applies Queue events to device-local Playback Session state", async () => {
		const api = createApi();
		vi.mocked(api.getQueue)
			.mockResolvedValueOnce({ items: queueItems, revision: "1" })
			.mockResolvedValueOnce({ items: [queueItems[1]], revision: "2" });
		const { engine } = renderPlayback(api);
		await act(async () => {});
		await act(async () =>
			screen.getByRole("button", { name: "Track" }).click(),
		);
		await act(async () => {
			engine.seek(42);
			engine.setVolume(0.3);
			engine.togglePlay();
		});

		await notifyQueueEvent(api, {
			revision: "2",
			sequence: "2",
			invalidates: ["queue"],
		});

		expect(engine.getState()).toMatchObject({
			source: { type: "track", track: { id: "track-1" } },
			status: "paused",
			currentTime: 42,
			volume: 0.3,
		});
	});

	it("refetches and retries unambiguous append intent once", async () => {
		const api = createApi();
		vi.mocked(api.getQueue)
			.mockResolvedValueOnce({ items: queueItems, revision: "1" })
			.mockResolvedValueOnce({ items: queueItems, revision: "2" });
		vi.mocked(api.appendQueueItem)
			.mockRejectedValueOnce(queueConflict("2"))
			.mockResolvedValueOnce({ items: queueItems, revision: "3" });
		renderPlayback(api);
		await act(async () => {});

		await act(async () =>
			screen.getByRole("button", { name: "Append" }).click(),
		);

		expect(api.appendQueueItem).toHaveBeenNthCalledWith(1, "track-2", "1");
		expect(api.appendQueueItem).toHaveBeenNthCalledWith(2, "track-2", "2");
		expect(api.getQueue).toHaveBeenCalledTimes(2);
	});

	it("refetches stale remove and treats already-removed intent as satisfied", async () => {
		const api = createApi();
		vi.mocked(api.getQueue)
			.mockResolvedValueOnce({ items: queueItems, revision: "1" })
			.mockResolvedValueOnce({ items: [queueItems[1]], revision: "2" });
		vi.mocked(api.removeQueueItem).mockRejectedValueOnce(queueConflict("2"));
		renderPlayback(api);

		await act(async () =>
			screen.getByRole("button", { name: "Remove" }).click(),
		);

		expect(api.removeQueueItem).toHaveBeenCalledTimes(1);
		expect(api.getQueue).toHaveBeenCalledTimes(2);
	});

	it("refetches and retries remove intent once when item still exists", async () => {
		const api = createApi();
		vi.mocked(api.getQueue)
			.mockResolvedValueOnce({ items: queueItems, revision: "1" })
			.mockResolvedValueOnce({ items: queueItems, revision: "2" });
		vi.mocked(api.removeQueueItem)
			.mockRejectedValueOnce(queueConflict("2"))
			.mockResolvedValueOnce({ items: [queueItems[1]], revision: "3" });
		renderPlayback(api);
		await act(async () => {});

		await act(async () =>
			screen.getByRole("button", { name: "Remove" }).click(),
		);

		expect(api.removeQueueItem).toHaveBeenNthCalledWith(1, "item-1", "1");
		expect(api.removeQueueItem).toHaveBeenNthCalledWith(2, "item-1", "2");
		expect(api.getQueue).toHaveBeenCalledTimes(2);
	});

	it.each([
		["replace", "Clear"],
		["reorder", "Reorder"],
	] as const)("does not auto-merge %s conflicts and exposes them", async (operation, buttonName) => {
		const api = createApi();
		vi.mocked(api.getQueue)
			.mockResolvedValueOnce({ items: queueItems, revision: "1" })
			.mockResolvedValueOnce({ items: queueItems, revision: "2" });
		if (operation === "replace") {
			vi.mocked(api.replaceQueue).mockRejectedValueOnce(queueConflict("2"));
		} else {
			vi.mocked(api.reorderQueue).mockRejectedValueOnce(queueConflict("2"));
		}
		renderPlayback(api);

		await act(async () =>
			screen.getByRole("button", { name: buttonName }).click(),
		);

		const mutation =
			operation === "replace" ? api.replaceQueue : api.reorderQueue;
		expect(mutation).toHaveBeenCalledTimes(1);
		expect(api.getQueue).toHaveBeenCalledTimes(2);
		expect(screen.getByTestId("queue-conflict").textContent).toContain(
			"changed",
		);
	});

	it("keeps the latest server Queue after an ambiguous reorder conflict", async () => {
		const api = createApi();
		vi.mocked(api.getQueue)
			.mockResolvedValueOnce({ items: queueItems, revision: "1" })
			.mockResolvedValueOnce({ items: [queueItems[1]], revision: "2" });
		vi.mocked(api.reorderQueue).mockRejectedValueOnce(queueConflict("2"));
		renderPlayback(api);

		await act(async () =>
			screen.getByRole("button", { name: "Reorder" }).click(),
		);

		expect(screen.getByTestId("queue").textContent).toBe("track-2");
		expect(screen.getByTestId("queue-conflict").textContent).toContain(
			"Review order",
		);
	});

	it.each([
		["append retry", "Append", true],
		["remove retry", "Remove", true],
		["already-satisfied remove", "Remove", false],
	] as const)("clears stale conflict after %s", async (_scenario, buttonName, shouldRetry) => {
		const api = createApi();
		const latestItems = shouldRetry ? queueItems : [queueItems[1]];
		vi.mocked(api.getQueue)
			.mockResolvedValueOnce({ items: queueItems, revision: "1" })
			.mockResolvedValueOnce({ items: queueItems, revision: "2" })
			.mockResolvedValueOnce({ items: latestItems, revision: "3" });
		vi.mocked(api.reorderQueue).mockRejectedValueOnce(queueConflict("2"));
		if (buttonName === "Append") {
			vi.mocked(api.appendQueueItem)
				.mockRejectedValueOnce(queueConflict("3"))
				.mockResolvedValueOnce({ items: queueItems, revision: "4" });
		} else {
			vi.mocked(api.removeQueueItem).mockRejectedValueOnce(queueConflict("3"));
			if (shouldRetry) {
				vi.mocked(api.removeQueueItem).mockResolvedValueOnce({
					items: [queueItems[1]],
					revision: "4",
				});
			}
		}
		renderPlayback(api);
		await act(async () => {});

		await act(async () =>
			screen.getByRole("button", { name: "Reorder" }).click(),
		);
		expect(screen.getByTestId("queue-conflict").textContent).toContain(
			"changed",
		);

		await act(async () =>
			screen.getByRole("button", { name: buttonName }).click(),
		);

		expect(screen.getByTestId("queue-conflict").textContent).toBe("");
	});
});

function queueConflict(revision: string) {
	return new ApiError(409, {
		error: "conflict",
		code: "queue_revision_conflict",
		message: "queue changed since supplied revision",
		queue: { items: queueItems, revision },
	});
}

async function notifyQueueEvent(api: PlaybackApi, event: QueueEvent) {
	if (!api.subscribeQueueEvents) {
		throw new Error("Queue event subscription is unavailable");
	}
	const subscribe = vi.mocked(api.subscribeQueueEvents);
	await act(async () => {
		subscribe.mock.calls[0]?.[0](event);
	});
}

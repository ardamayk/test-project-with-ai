import type {
	RadioNowPlaying,
	RadioSearchResult,
	RadioStation,
} from "@repo/api-client";
import { act, cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	type PlaybackApi,
	PlaybackProvider,
	usePlayback,
} from "./PlaybackProvider";

const hlsMocks = vi.hoisted(() => ({
	isSupported: vi.fn(() => true),
	instances: [] as Array<{
		attachMedia: ReturnType<typeof vi.fn>;
		destroy: ReturnType<typeof vi.fn>;
		loadSource: ReturnType<typeof vi.fn>;
	}>,
}));

vi.mock("hls.js", () => ({
	default: class HlsMock {
		static isSupported = hlsMocks.isSupported;
		attachMedia = vi.fn();
		destroy = vi.fn();
		loadSource = vi.fn();

		constructor() {
			hlsMocks.instances.push(this);
		}
	},
}));

const track = {
	id: "track-1",
	title: "Track 1",
	artistName: "Artist",
	albumId: "album-1",
	durationMs: 120000,
	format: "flac",
};

const track2 = {
	id: "track-2",
	title: "Track 2",
	artistName: "Artist",
	albumId: "album-1",
	durationMs: 180000,
	format: "flac",
};

const playlistApi = {
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

const radioStation: RadioStation = {
	id: "station-1",
	name: "Station 1",
	streamUrl: "https://example.com/live",
	tags: [],
	source: "manual",
	isFavorite: false,
	position: 0,
};

const hlsRadioStation: RadioStation = {
	...radioStation,
	id: "station-hls",
	streamUrl: "https://example.com/live/chunks.m3u8",
};

const hlsCatalogResult: RadioSearchResult = {
	stationUuid: "catalog-hls",
	name: "Catalog HLS",
	streamUrl: "https://example.com/catalog/chunks.m3u8",
	tags: [],
};

const radioNowPlaying: RadioNowPlaying = {
	raw: "Artist - Song",
	title: "Song",
	artist: "Artist",
};

const radioApi = {
	getRadioStationStreamUrl: (stationId: string) => `/radio/${stationId}`,
	getRadioCatalogPreviewStreamUrl: (stationUuid: string) =>
		`/radio/preview/${stationUuid}`,
	getRadioNowPlaying: vi.fn(async () => radioNowPlaying),
};

class AudioMock extends EventTarget {
	static instances: AudioMock[] = [];

	currentTime = 0;
	duration = 120;
	paused = true;
	src = "";
	volume = 1;
	canPlayType = vi.fn(() => "");
	pause = vi.fn(() => {
		this.paused = true;
		this.dispatchEvent(new Event("pause"));
	});
	play = vi.fn(async () => {
		this.paused = false;
		this.dispatchEvent(new Event("play"));
	});
	removeAttribute = vi.fn((name: string) => {
		if (name === "src") {
			this.src = "";
		}
	});

	constructor() {
		super();
		AudioMock.instances.push(this);
	}
}

function createApi(): PlaybackApi {
	return {
		getQueue: vi.fn(async () => ({
			items: [{ id: "item-1", trackId: track.id, position: 0, track }],
		})),
		replaceQueue: vi.fn(async () => ({
			items: [{ id: "item-1", trackId: track.id, position: 0, track }],
		})),
		appendQueueItem: vi.fn(async () => ({
			items: [{ id: "item-1", trackId: track.id, position: 0, track }],
		})),
		removeQueueItem: vi.fn(async () => ({ items: [] })),
		getStreamUrl: (trackId) => `/stream/${trackId}`,
		getAlbumCoverUrl: (albumId) => `/cover/${albumId}`,
		...radioApi,
		...playlistApi,
	};
}

function createEmptyQueueApi(): PlaybackApi {
	return {
		getQueue: vi.fn(async () => ({ items: [] })),
		replaceQueue: vi.fn(async () => ({
			items: [
				{ id: "item-1", trackId: track.id, position: 0, track },
				{ id: "item-2", trackId: track2.id, position: 1, track: track2 },
			],
		})),
		appendQueueItem: vi.fn(async () => ({
			items: [{ id: "item-1", trackId: track.id, position: 0, track }],
		})),
		removeQueueItem: vi.fn(async () => ({ items: [] })),
		getStreamUrl: (trackId) => `/stream/${trackId}`,
		getAlbumCoverUrl: (albumId) => `/cover/${albumId}`,
		...radioApi,
		...playlistApi,
	};
}

function createAppendAlbumApi(): PlaybackApi {
	let items = [{ id: "item-1", trackId: track.id, position: 0, track }];

	return {
		getQueue: vi.fn(async () => ({ items })),
		replaceQueue: vi.fn(async () => ({ items })),
		appendQueueItem: vi.fn(async (trackId) => {
			const nextTrack = trackId === track2.id ? track2 : track;
			items = [
				...items,
				{
					id: `item-${items.length + 1}`,
					trackId,
					position: items.length,
					track: nextTrack,
				},
			];
			return { items };
		}),
		removeQueueItem: vi.fn(async () => ({ items: [] })),
		getStreamUrl: (trackId) => `/stream/${trackId}`,
		getAlbumCoverUrl: (albumId) => `/cover/${albumId}`,
		...radioApi,
		...playlistApi,
	};
}

function createPlayNextApi(): PlaybackApi {
	const items = [
		{ id: "item-1", trackId: track.id, position: 0, track },
		{ id: "item-2", trackId: track2.id, position: 1, track: track2 },
	];

	return {
		getQueue: vi.fn(async () => ({ items })),
		replaceQueue: vi.fn(async (trackIds: string[]) => ({
			items: trackIds.map((trackId, index) => {
				const nextTrack = trackId === track2.id ? track2 : track;
				return {
					id: `item-${index + 1}`,
					trackId,
					position: index,
					track: nextTrack,
				};
			}),
		})),
		appendQueueItem: vi.fn(async () => ({ items })),
		removeQueueItem: vi.fn(async () => ({ items: [] })),
		getStreamUrl: (trackId) => `/stream/${trackId}`,
		getAlbumCoverUrl: (albumId) => `/cover/${albumId}`,
		...radioApi,
		...playlistApi,
	};
}

function Harness({ children }: { children?: ReactNode }) {
	const playback = usePlayback();
	return (
		<div>
			<span data-testid="volume">{playback.volume}</span>
			<span data-testid="playing">{String(playback.isPlaying)}</span>
			<span data-testid="current-track">
				{playback.currentTrack?.title ?? ""}
			</span>
			<span data-testid="current-radio">
				{playback.currentRadioStation?.name ?? ""}
			</span>
			<span data-testid="radio-now-playing">
				{playback.radioNowPlaying?.raw ?? ""}
			</span>
			<span data-testid="repeat-mode">{playback.repeatMode}</span>
			<span data-testid="shuffle-enabled">
				{String(playback.shuffleEnabled)}
			</span>
			<button type="button" onClick={() => playback.setVolume(0.3)}>
				Set volume
			</button>
			<button type="button" onClick={() => void playback.playTrack(track.id)}>
				Play
			</button>
			<button
				type="button"
				onClick={() => void playback.playRadioStation(radioStation)}
			>
				Play radio
			</button>
			<button
				type="button"
				onClick={() => void playback.playRadioStation(hlsRadioStation)}
			>
				Play HLS radio
			</button>
			<button
				type="button"
				onClick={() => void playback.playRadioCatalogPreview(hlsCatalogResult)}
			>
				Preview HLS radio
			</button>
			<button
				type="button"
				onClick={() => void playback.playTrack(track.id, [track.id, track2.id])}
			>
				Play album
			</button>
			<button
				type="button"
				onClick={() => void playback.queueTracks([track2.id])}
			>
				Queue album
			</button>
			<button type="button" onClick={playback.cycleRepeatMode}>
				Repeat
			</button>
			<button type="button" onClick={playback.toggleShuffle}>
				Shuffle
			</button>
			<button type="button" onClick={() => void playback.playNext(track2.id)}>
				Play next
			</button>
			{children}
		</div>
	);
}

describe("PlaybackProvider", () => {
	const originalAudio = globalThis.Audio;

	beforeEach(() => {
		AudioMock.instances = [];
		hlsMocks.instances = [];
		hlsMocks.isSupported.mockReturnValue(true);
		globalThis.Audio = AudioMock as unknown as typeof Audio;
	});

	afterEach(() => {
		cleanup();
		globalThis.Audio = originalAudio;
	});

	it("updates volume without recreating the audio element", async () => {
		render(
			<PlaybackProvider api={createApi()}>
				<Harness />
			</PlaybackProvider>,
		);

		await screen.findByTestId("volume");
		expect(AudioMock.instances).toHaveLength(1);

		await act(async () => {
			screen.getByRole("button", { name: "Set volume" }).click();
		});

		expect(screen.getByTestId("volume").textContent).toBe("0.3");
		expect(AudioMock.instances).toHaveLength(1);
		expect(AudioMock.instances[0]?.volume).toBe(0.3);
	});

	it("plays a queued track through the stream URL", async () => {
		render(
			<PlaybackProvider api={createApi()}>
				<Harness />
			</PlaybackProvider>,
		);

		await act(async () => {
			screen.getByRole("button", { name: "Play" }).click();
		});

		expect(AudioMock.instances[0]?.src).toBe("/stream/track-1");
		expect(AudioMock.instances[0]?.play).toHaveBeenCalledOnce();
		expect(screen.getByTestId("playing").textContent).toBe("true");
	});

	it("plays a radio station without replacing the queue", async () => {
		const api = createApi();
		render(
			<PlaybackProvider api={api}>
				<Harness />
			</PlaybackProvider>,
		);

		await act(async () => {
			screen.getByRole("button", { name: "Play radio" }).click();
		});

		expect(AudioMock.instances[0]?.src).toBe("/radio/station-1");
		expect(AudioMock.instances[0]?.play).toHaveBeenCalledOnce();
		expect(api.replaceQueue).not.toHaveBeenCalled();
		expect(screen.getByTestId("current-track").textContent).toBe("");
		expect(screen.getByTestId("current-radio").textContent).toBe("Station 1");
		expect(screen.getByTestId("radio-now-playing").textContent).toBe(
			"Artist - Song",
		);
	});

	it("plays HLS radio streams through hls.js when native HLS is unavailable", async () => {
		render(
			<PlaybackProvider api={createApi()}>
				<Harness />
			</PlaybackProvider>,
		);

		await act(async () => {
			screen.getByRole("button", { name: "Play HLS radio" }).click();
		});

		expect(hlsMocks.instances).toHaveLength(1);
		expect(hlsMocks.instances[0]?.loadSource).toHaveBeenCalledWith(
			"https://example.com/live/chunks.m3u8",
		);
		expect(hlsMocks.instances[0]?.attachMedia).toHaveBeenCalledWith(
			AudioMock.instances[0],
		);
		expect(AudioMock.instances[0]?.src).toBe("");
		expect(AudioMock.instances[0]?.play).toHaveBeenCalledOnce();
	});

	it("prefers hls.js for HLS streams when native support is only maybe", async () => {
		render(
			<PlaybackProvider api={createApi()}>
				<Harness />
			</PlaybackProvider>,
		);
		AudioMock.instances[0]?.canPlayType.mockReturnValue("maybe");

		await act(async () => {
			screen.getByRole("button", { name: "Play HLS radio" }).click();
		});

		expect(hlsMocks.instances).toHaveLength(1);
		expect(hlsMocks.instances[0]?.loadSource).toHaveBeenCalledWith(
			"https://example.com/live/chunks.m3u8",
		);
		expect(AudioMock.instances[0]?.src).toBe("");
	});

	it("uses the catalog stream URL for HLS previews", async () => {
		render(
			<PlaybackProvider api={createApi()}>
				<Harness />
			</PlaybackProvider>,
		);

		await act(async () => {
			screen.getByRole("button", { name: "Preview HLS radio" }).click();
		});

		expect(hlsMocks.instances).toHaveLength(1);
		expect(hlsMocks.instances[0]?.loadSource).toHaveBeenCalledWith(
			"https://example.com/catalog/chunks.m3u8",
		);
		expect(AudioMock.instances[0]?.src).toBe("");
		expect(screen.getByTestId("current-radio").textContent).toBe("Catalog HLS");
	});

	it("sets the current track when replacing the queue before playback", async () => {
		render(
			<PlaybackProvider api={createEmptyQueueApi()}>
				<Harness />
			</PlaybackProvider>,
		);

		await act(async () => {
			screen.getByRole("button", { name: "Play album" }).click();
		});

		expect(screen.getByTestId("current-track").textContent).toBe("Track 1");
		expect(AudioMock.instances[0]?.src).toBe("/stream/track-1");
	});

	it("appends album tracks without changing the current track", async () => {
		const api = createAppendAlbumApi();
		render(
			<PlaybackProvider api={api}>
				<Harness />
			</PlaybackProvider>,
		);

		await act(async () => {
			screen.getByRole("button", { name: "Play" }).click();
		});
		await act(async () => {
			screen.getByRole("button", { name: "Queue album" }).click();
		});

		expect(api.appendQueueItem).toHaveBeenCalledWith(track2.id);
		expect(screen.getByTestId("current-track").textContent).toBe("Track 1");
		expect(AudioMock.instances[0]?.src).toBe("/stream/track-1");
	});

	it("cycles repeat modes and toggles shuffle state", async () => {
		render(
			<PlaybackProvider api={createApi()}>
				<Harness />
			</PlaybackProvider>,
		);

		expect(screen.getByTestId("repeat-mode").textContent).toBe("off");
		expect(screen.getByTestId("shuffle-enabled").textContent).toBe("false");

		await act(async () => {
			screen.getByRole("button", { name: "Repeat" }).click();
		});
		expect(screen.getByTestId("repeat-mode").textContent).toBe("once");

		await act(async () => {
			screen.getByRole("button", { name: "Repeat" }).click();
		});
		expect(screen.getByTestId("repeat-mode").textContent).toBe("loop");

		await act(async () => {
			screen.getByRole("button", { name: "Repeat" }).click();
			screen.getByRole("button", { name: "Shuffle" }).click();
		});
		expect(screen.getByTestId("repeat-mode").textContent).toBe("off");
		expect(screen.getByTestId("shuffle-enabled").textContent).toBe("true");
	});

	it("replays once then clears repeat once mode", async () => {
		render(
			<PlaybackProvider api={createApi()}>
				<Harness />
			</PlaybackProvider>,
		);

		await act(async () => {
			screen.getByRole("button", { name: "Play" }).click();
			screen.getByRole("button", { name: "Repeat" }).click();
		});

		await act(async () => {
			AudioMock.instances[0]?.dispatchEvent(new Event("ended"));
		});

		expect(AudioMock.instances[0]?.play).toHaveBeenCalledTimes(2);
		expect(screen.getByTestId("repeat-mode").textContent).toBe("off");
	});

	it("keeps replaying in repeat loop mode", async () => {
		render(
			<PlaybackProvider api={createApi()}>
				<Harness />
			</PlaybackProvider>,
		);

		await act(async () => {
			screen.getByRole("button", { name: "Play" }).click();
			screen.getByRole("button", { name: "Repeat" }).click();
			screen.getByRole("button", { name: "Repeat" }).click();
		});

		await act(async () => {
			AudioMock.instances[0]?.dispatchEvent(new Event("ended"));
			AudioMock.instances[0]?.dispatchEvent(new Event("ended"));
		});

		expect(AudioMock.instances[0]?.play).toHaveBeenCalledTimes(3);
		expect(screen.getByTestId("repeat-mode").textContent).toBe("loop");
	});

	it("inserts a track after the current queue position via playNext", async () => {
		const api = createPlayNextApi();
		render(
			<PlaybackProvider api={api}>
				<Harness />
			</PlaybackProvider>,
		);

		await act(async () => {
			screen.getByRole("button", { name: "Play" }).click();
		});
		await act(async () => {
			screen.getByRole("button", { name: "Play next" }).click();
		});

		expect(api.replaceQueue).toHaveBeenCalledWith([track.id, track2.id]);
	});
});

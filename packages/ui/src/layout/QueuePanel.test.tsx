import {
	act,
	cleanup,
	fireEvent,
	render,
	screen,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
	type PlaybackApi,
	PlaybackProvider,
} from "../playback/PlaybackProvider";
import { InMemoryPlaybackEngine } from "../playback/testing/InMemoryPlaybackEngine";
import { defaultPreferences } from "../widgets/types";
import { LayoutProvider } from "./LayoutProvider";
import { QueuePanel } from "./QueuePanel";

const track = {
	id: "track-1",
	title: "Track 1",
	artistName: "Artist",
	artists: [],
	albumId: "album-1",
	discNo: 1,
	durationMs: 120000,
	format: "opus",
	genres: [],
};

const track2 = { ...track, id: "track-2", title: "Track 2" };
const track3 = { ...track, id: "track-3", title: "Track 3" };

const threeItems = [
	{ id: "item-1", trackId: track.id, position: 0, track },
	{ id: "item-2", trackId: track2.id, position: 1, track: track2 },
	{ id: "item-3", trackId: track3.id, position: 2, track: track3 },
];

function createApi(
	items = [{ id: "item-1", trackId: track.id, position: 0, track }],
): PlaybackApi {
	return {
		getQueue: vi.fn(async () => ({ items, revision: "1" })),
		replaceQueue: vi.fn(async () => ({ items: [], revision: "2" })),
		reorderQueue: vi.fn(async () => ({ items: [], revision: "2" })),
		appendQueueItem: vi.fn(async () => ({ items: [], revision: "2" })),
		removeQueueItem: vi.fn(async () => ({ items: [], revision: "2" })),
		getStreamUrl: (trackId) => `/stream/${trackId}`,
		getAlbumCoverUrl: (albumId) => `/cover/${albumId}`,
		getRadioStationStreamUrl: (stationId) => `/radio/${stationId}`,
		getRadioCatalogPreviewStreamUrl: (stationUuid) =>
			`/radio/preview/${stationUuid}`,
		getRadioNowPlaying: vi.fn(async () => ({})),
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

describe("QueuePanel", () => {
	afterEach(cleanup);

	it("plays a track when left-clicking the queue row", async () => {
		const engine = new InMemoryPlaybackEngine();
		render(
			<LayoutProvider initialPreferences={defaultPreferences}>
				<PlaybackProvider api={createApi()} engine={engine}>
					<QueuePanel />
				</PlaybackProvider>
			</LayoutProvider>,
		);

		const title = await screen.findByText("Track 1");
		const row = title.closest("li");

		await act(async () => {
			fireEvent.click(row as HTMLElement);
		});

		expect(engine.getState().source).toMatchObject({
			type: "track",
			playbackUrl: "/stream/track-1",
		});
	});

	it("splits the Queue into played, playing and next up around the current item", async () => {
		const engine = new InMemoryPlaybackEngine();
		render(
			<LayoutProvider initialPreferences={defaultPreferences}>
				<PlaybackProvider api={createApi(threeItems)} engine={engine}>
					<QueuePanel />
				</PlaybackProvider>
			</LayoutProvider>,
		);

		expect(await screen.findByText("Track 2")).toBeTruthy();
		expect(screen.getByRole("region", { name: "Next up" })).toBeTruthy();
		expect(screen.queryByRole("region", { name: "Played" })).toBeNull();

		await act(async () => {
			fireEvent.click(screen.getByText("Track 2").closest("li") as HTMLElement);
		});

		const played = screen.getByRole("region", { name: "Played" });
		const playing = screen.getByRole("region", { name: "Playing" });
		const nextUp = screen.getByRole("region", { name: "Next up" });
		expect(played.textContent).toContain("Track 1");
		expect(playing.textContent).toContain("Track 2");
		expect(nextUp.textContent).toContain("Track 3");
		expect(
			playing.querySelector('[aria-current="true"]')?.textContent,
		).toContain("Track 2");

		// Going back: a played row is still playable.
		await act(async () => {
			fireEvent.click(screen.getByText("Track 1").closest("li") as HTMLElement);
		});
		expect(screen.queryByRole("region", { name: "Played" })).toBeNull();
		expect(
			screen.getByRole("region", { name: "Playing" }).textContent,
		).toContain("Track 1");
		expect(engine.getState().source).toMatchObject({
			type: "track",
			playbackUrl: "/stream/track-1",
		});
	});
});

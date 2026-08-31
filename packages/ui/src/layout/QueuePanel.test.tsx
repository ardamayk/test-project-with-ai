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

function createApi(): PlaybackApi {
	return {
		getQueue: vi.fn(async () => ({
			items: [{ id: "item-1", trackId: track.id, position: 0, track }],
			revision: "1",
		})),
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
});

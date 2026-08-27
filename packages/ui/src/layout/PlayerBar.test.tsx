import type { RadioStation } from "@repo/api-client";
import {
	act,
	cleanup,
	fireEvent,
	render,
	screen,
	within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	type PlaybackApi,
	PlaybackProvider,
	usePlayback,
} from "../playback/PlaybackProvider";
import { InMemoryPlaybackEngine } from "../playback/testing/InMemoryPlaybackEngine";
import { defaultPreferences } from "../widgets/types";
import { LayoutProvider } from "./LayoutProvider";
import { PlayerBar } from "./PlayerBar";

const navigate = vi.fn();

vi.mock("@tanstack/react-router", () => ({
	useNavigate: () => navigate,
}));

const track = {
	id: "track-1",
	title: "Track 1",
	artistName: "Artist",
	albumId: "album-1",
	albumTitle: "Album 1",
	durationMs: 120000,
	format: "flac",
	sampleRateHz: 96000,
	bitrateKbps: 1411,
	replayGain: {
		trackGainDb: -7.25,
		trackPeak: null,
		albumGainDb: null,
		albumPeak: null,
	},
};

const radioStation: RadioStation = {
	id: "station-1",
	name: "Radio Paradise Main Mix",
	streamUrl: "https://example.com/radio.mp3",
	faviconUrl: "https://example.com/radio.png",
	country: "United States",
	language: "English",
	tags: ["eclectic"],
	source: "manual",
	isFavorite: false,
	position: 0,
};

const api: PlaybackApi = {
	getQueue: vi.fn(async () => ({
		items: [{ id: "item-1", trackId: track.id, position: 0, track }],
		revision: "1",
	})),
	replaceQueue: vi.fn(async () => ({
		items: [{ id: "item-1", trackId: track.id, position: 0, track }],
		revision: "2",
	})),
	reorderQueue: vi.fn(async () => ({
		items: [{ id: "item-1", trackId: track.id, position: 0, track }],
		revision: "2",
	})),
	appendQueueItem: vi.fn(async () => ({
		items: [{ id: "item-1", trackId: track.id, position: 0, track }],
		revision: "2",
	})),
	removeQueueItem: vi.fn(async () => ({ items: [], revision: "2" })),
	getStreamUrl: (trackId) => `/stream/${trackId}`,
	getAlbumCoverUrl: (albumId) => `/cover/${albumId}`,
	getRadioStationStreamUrl: (stationId) => `/radio/${stationId}`,
	getRadioCatalogPreviewStreamUrl: (stationUuid) =>
		`/radio/preview/${stationUuid}`,
	getRadioNowPlaying: vi.fn(async () => ({})),
	listPlaylists: vi.fn(async () => ({
		items: [
			{ id: "favorites", name: "Favorites", isDefault: true, trackCount: 0 },
			{ id: "road", name: "Road", isDefault: false, trackCount: 1 },
		],
		total: 2,
	})),
	getPlaylist: vi.fn(async (playlistId: string) => ({
		id: playlistId,
		name: playlistId === "favorites" ? "Favorites" : "Road",
		isDefault: playlistId === "favorites",
		trackCount: playlistId === "favorites" ? 1 : 0,
		tracks: playlistId === "favorites" ? [track] : [],
	})),
	createPlaylist: vi.fn(async (name) => ({
		id: "new",
		name,
		isDefault: false,
		trackCount: 0,
	})),
	addPlaylistTrack: vi.fn(async (playlistId) => ({
		id: playlistId,
		name: playlistId === "favorites" ? "Favorites" : "Road",
		isDefault: playlistId === "favorites",
		trackCount: 1,
		tracks: [track],
	})),
	removePlaylistTrack: vi.fn(async (playlistId) => ({
		id: playlistId,
		name: playlistId === "favorites" ? "Favorites" : "Road",
		isDefault: playlistId === "favorites",
		trackCount: 0,
		tracks: [],
	})),
};

function PlaybackStarter() {
	const playback = usePlayback();
	return (
		<button type="button" onClick={() => void playback.playTrack(track.id)}>
			Start track
		</button>
	);
}

function RadioStarter() {
	const playback = usePlayback();
	return (
		<button
			type="button"
			onClick={() => void playback.playRadioStation(radioStation)}
		>
			Start radio
		</button>
	);
}

function renderPlayerBar(onPlaylistMutated?: () => void) {
	return render(
		<LayoutProvider initialPreferences={defaultPreferences}>
			<PlaybackProvider api={api} engine={new InMemoryPlaybackEngine()}>
				<PlaybackStarter />
				<RadioStarter />
				<PlayerBar onPlaylistMutated={onPlaylistMutated} />
			</PlaybackProvider>
		</LayoutProvider>,
	);
}

async function openActionsMenu() {
	await act(async () => {
		screen.getByRole("button", { name: "Start track" }).click();
	});
	fireEvent.click(screen.getByRole("button", { name: "Track actions" }));
}

async function openPlaylistSubmenu() {
	await openActionsMenu();
	fireEvent.mouseEnter(
		screen.getByRole("menuitem", { name: "Add to playlist" }),
	);
}

describe("PlayerBar", () => {
	beforeEach(() => {
		navigate.mockClear();
	});

	afterEach(() => {
		cleanup();
		vi.clearAllMocks();
	});

	it("renders an empty disabled playback state", () => {
		renderPlayerBar();

		expect(screen.getByText("Nothing playing")).toBeTruthy();
		expect(screen.getByText("Select a track")).toBeTruthy();
		expect(
			(screen.getByRole("button", { name: "Play" }) as HTMLButtonElement)
				.disabled,
		).toBe(true);
	});

	it("shows the current track after playback starts", async () => {
		renderPlayerBar();

		await act(async () => {
			screen.getByRole("button", { name: "Start track" }).click();
		});

		expect(screen.getByText("Track 1")).toBeTruthy();
		expect(screen.getByText("Artist")).toBeTruthy();
		expect(screen.getByText("Album 1")).toBeTruthy();
	});

	it("keeps the now-playing region at a stable width independent of title length", async () => {
		renderPlayerBar();

		await act(async () => {
			screen.getByRole("button", { name: "Start track" }).click();
		});

		const nowPlaying = screen.getByLabelText("Now playing");
		expect(nowPlaying.className).toContain("min-w-[200px]");
		expect(nowPlaying.className).toContain("flex-[1_0_0]");
	});

	it("keeps playback controls centered in the full player bar", async () => {
		renderPlayerBar();

		const controls = screen.getByLabelText("Playback controls");
		expect(controls.parentElement?.className).toContain("justify-between");
		expect(controls.className).toContain("justify-self-center");
	});

	it("renders the Figma player bar shell dimensions", () => {
		renderPlayerBar();

		expect(screen.getByRole("contentinfo").className).toContain("h-[72px]");
		expect(screen.getByRole("contentinfo").className).toContain("bg-player");
	});

	it("keeps volume slider usable", async () => {
		renderPlayerBar();

		fireEvent.change(screen.getByLabelText("Volume"), {
			target: { value: "0.3" },
		});
		expect((screen.getByLabelText("Volume") as HTMLInputElement).value).toBe(
			"0.3",
		);

		fireEvent.change(screen.getByLabelText("Volume"), {
			target: { value: "0" },
		});
		expect((screen.getByLabelText("Volume") as HTMLInputElement).value).toBe(
			"0",
		);
	});

	it("renders bitrate and sample rate quality details with track actions menu", async () => {
		renderPlayerBar();
		await openActionsMenu();

		expect(
			screen.getByRole("button", { name: "Quality 1411 kbps · 96 kHz" }),
		).toBeTruthy();
		expect(screen.getByText("1411 kbps · 96 kHz")).toBeTruthy();
		expect(screen.getByText("Add to playlist")).toBeTruthy();
		expect(screen.queryByText("Play next")).toBeNull();
		expect(screen.getByText("Go to album")).toBeTruthy();
		expect(screen.getByText("Go to artist")).toBeTruthy();
		expect(
			(screen.getByRole("menuitem", { name: "Download" }) as HTMLButtonElement)
				.disabled,
		).toBe(true);
		expect(screen.getByRole("menuitem", { name: "Details" })).toBeTruthy();
	});

	it("shows live radio progress instead of track time", async () => {
		renderPlayerBar();

		await act(async () => {
			screen.getByRole("button", { name: "Start radio" }).click();
		});

		expect(screen.getByText("LIVE")).toBeTruthy();
		expect(screen.getByText("--:--")).toBeTruthy();
		expect(screen.queryByLabelText("Seek")).toBeNull();
		expect(
			screen.getByRole("button", { name: "Quality High Quality" }),
		).toBeTruthy();
	});

	it("renders the actions menu in a portal with anchored coordinates", async () => {
		const rect = {
			top: 320,
			left: 48,
			right: 76,
			bottom: 348,
			width: 28,
			height: 28,
			x: 48,
			y: 320,
			toJSON: () => ({}),
		};
		vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(
			rect,
		);

		renderPlayerBar();
		await openActionsMenu();

		const menu = document.getElementById("player-track-actions-menu");
		expect(menu).toBeTruthy();
		expect(menu?.parentElement).toBe(document.body);
		expect(menu?.style.top).toBe("312px");
		expect(menu?.style.left).toBe("48px");
	});

	it("navigates to album and artist from the actions menu", async () => {
		renderPlayerBar();
		await openActionsMenu();

		fireEvent.click(screen.getByRole("menuitem", { name: "Go to album" }));
		expect(navigate).toHaveBeenCalledWith({
			to: "/library/$albumId",
			params: { albumId: "album-1" },
		});

		await openActionsMenu();
		fireEvent.click(screen.getByRole("menuitem", { name: "Go to artist" }));
		expect(navigate).toHaveBeenCalledWith({
			to: "/library/artists",
			search: { q: "Artist" },
		});
	});

	it("opens a hover submenu for add to playlist and toggles membership", async () => {
		const onPlaylistMutated = vi.fn();
		renderPlayerBar(onPlaylistMutated);
		await openPlaylistSubmenu();

		const addToPlaylist = screen.getByRole("menuitem", {
			name: "Add to playlist",
		});
		expect(addToPlaylist.querySelector(".lucide-plus")).toBeNull();
		expect(screen.getByPlaceholderText("Search playlists")).toBeTruthy();
		expect(
			screen.getByRole("menuitem", { name: "Create new playlist" }),
		).toBeTruthy();
		expect(await screen.findByText("Favorites")).toBeTruthy();
		expect(
			screen.getByRole("menuitem", { name: "Remove from Favorites" }),
		).toBeTruthy();

		await act(async () => {
			fireEvent.click(
				screen.getByRole("menuitem", { name: "Remove from Favorites" }),
			);
		});

		expect(api.removePlaylistTrack).toHaveBeenCalledWith("favorites", track.id);
		expect(onPlaylistMutated).toHaveBeenCalled();

		await act(async () => {
			fireEvent.click(
				screen.getByRole("menuitem", { name: "Add to Favorites" }),
			);
		});

		expect(api.addPlaylistTrack).toHaveBeenCalledWith("favorites", track.id);
	});

	it("keeps the add-to-playlist submenu open while moving across the hover gap", async () => {
		vi.useFakeTimers();
		renderPlayerBar();
		await openPlaylistSubmenu();

		const addToPlaylist = screen.getByRole("menuitem", {
			name: "Add to playlist",
		});
		const wrapper = addToPlaylist.parentElement;
		expect(wrapper).toBeTruthy();
		fireEvent.mouseLeave(wrapper as HTMLElement);
		expect(screen.getByRole("menu", { name: "Add to playlist" })).toBeTruthy();

		fireEvent.mouseEnter(screen.getByRole("menu", { name: "Add to playlist" }));
		await act(async () => {
			vi.advanceTimersByTime(250);
		});

		expect(screen.getByRole("menu", { name: "Add to playlist" })).toBeTruthy();
		vi.useRealTimers();
	});

	it("creates a playlist from the add-to-playlist submenu", async () => {
		renderPlayerBar();
		await openPlaylistSubmenu();
		fireEvent.click(
			screen.getByRole("menuitem", { name: "Create new playlist" }),
		);
		fireEvent.change(screen.getByLabelText("New playlist name"), {
			target: { value: "Late night" },
		});

		await act(async () => {
			fireEvent.click(screen.getByRole("button", { name: "Create" }));
		});

		expect(api.createPlaylist).toHaveBeenCalledWith("Late night");
		expect(api.addPlaylistTrack).toHaveBeenCalledWith("new", track.id);
	});

	it("filters playlists while searching in the submenu", async () => {
		renderPlayerBar();
		await openPlaylistSubmenu();

		fireEvent.change(screen.getByPlaceholderText("Search playlists"), {
			target: { value: "road" },
		});

		expect(screen.getByText("Road")).toBeTruthy();
		expect(screen.queryByText("Recent")).toBeNull();
		expect(screen.queryByText("Favorites")).toBeNull();
	});

	it("opens a track info modal from the actions menu", async () => {
		renderPlayerBar();
		await openActionsMenu();
		fireEvent.click(screen.getByRole("menuitem", { name: "Details" }));

		expect(screen.getByRole("dialog", { name: "Track 1" })).toBeTruthy();
		const dialog = screen.getByRole("dialog", { name: "Track 1" });
		expect(within(dialog).getByText("Title")).toBeTruthy();
		expect(within(dialog).getByText("Album 1")).toBeTruthy();
		expect(within(dialog).getByText("1411 kbps")).toBeTruthy();
		expect(within(dialog).getByText("96 kHz")).toBeTruthy();
		expect(within(dialog).getByText("Track ReplayGain")).toBeTruthy();
		expect(within(dialog).getByText("Available · Gain -7.25 dB")).toBeTruthy();
		expect(within(dialog).getByText("Album ReplayGain")).toBeTruthy();
		expect(within(dialog).getByText("Unavailable")).toBeTruthy();
	});

	it("shows active shuffle and repeat states", async () => {
		renderPlayerBar();

		await act(async () => {
			screen.getByRole("button", { name: "Start track" }).click();
		});

		fireEvent.click(screen.getByRole("button", { name: "Shuffle off" }));
		expect(screen.getByRole("button", { name: "Shuffle on" })).toBeTruthy();

		fireEvent.click(screen.getByRole("button", { name: "Repeat off" }));
		expect(screen.getByRole("button", { name: "Repeat once" })).toBeTruthy();
		expect(screen.getByText("1")).toBeTruthy();

		fireEvent.click(screen.getByRole("button", { name: "Repeat once" }));
		expect(screen.getByRole("button", { name: "Repeat loop" })).toBeTruthy();
		expect(screen.getByLabelText("Repeat infinitely")).toBeTruthy();
	});
});

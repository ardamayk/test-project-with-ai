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
	artists: [],
	albumId: "album-1",
	discNo: 1,
	albumTitle: "Album 1",
	durationMs: 120000,
	format: "flac",
	genres: [],
	sampleRateHz: 96000,
	bitDepth: 24,
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
	getTrackLyrics: vi.fn(async () => ({ lyrics: "Line one\nLine two" })),
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

function renderPlayerBar(
	onPlaylistMutated?: () => void,
	engine = new InMemoryPlaybackEngine(),
) {
	const result = render(
		<LayoutProvider initialPreferences={defaultPreferences}>
			<PlaybackProvider api={api} engine={engine}>
				<PlaybackStarter />
				<RadioStarter />
				<PlayerBar onPlaylistMutated={onPlaylistMutated} />
			</PlaybackProvider>
		</LayoutProvider>,
	);
	return { ...result, engine };
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
		// Nothing is playing: the quality pill is a plain status, not a menu.
		expect(screen.queryByRole("button", { name: /^Quality/ })).toBeNull();
		expect(screen.getByLabelText("Quality Quality")).toBeTruthy();
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

	it("shows an active reconnecting state for live radio", async () => {
		const { engine } = renderPlayerBar();

		await act(async () => {
			screen.getByRole("button", { name: "Start radio" }).click();
		});
		act(() => engine.reconnect());

		expect(screen.getByRole("status").textContent).toBe("Reconnecting…");
		expect(screen.getByRole("button", { name: "Pause" })).toBeTruthy();
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

		expect(screen.getByRole("contentinfo").className).toContain("h-[80px]");
		expect(screen.getByRole("contentinfo").className).toContain("bg-player");
	});

	it("toggles the current Track's favorite state from the Player Bar", async () => {
		const onToggleFavorite = vi.fn();
		render(
			<LayoutProvider initialPreferences={defaultPreferences}>
				<PlaybackProvider api={api} engine={new InMemoryPlaybackEngine()}>
					<PlaybackStarter />
					<PlayerBar
						isCurrentTrackFavorite={false}
						onToggleFavorite={onToggleFavorite}
					/>
				</PlaybackProvider>
			</LayoutProvider>,
		);
		expect(
			(
				screen.getByRole("button", {
					name: "Add to favorites",
				}) as HTMLButtonElement
			).disabled,
		).toBe(true);

		await act(async () => {
			screen.getByRole("button", { name: "Start track" }).click();
		});
		fireEvent.click(screen.getByRole("button", { name: "Add to favorites" }));
		expect(onToggleFavorite).toHaveBeenCalledWith(track.id);
	});

	it("renders a round primary play control and a thin seek bar", async () => {
		renderPlayerBar();
		await act(async () => {
			screen.getByRole("button", { name: "Start track" }).click();
		});
		expect(screen.getByRole("button", { name: "Pause" }).className).toContain(
			"rounded-full",
		);
		const seek = screen.getByLabelText("Seek") as HTMLInputElement;
		expect(seek.className).toContain("player-seek-slider");
		expect(seek.style.getPropertyValue("--seek-level")).toMatch(/%$/);
	});

	it("opens the full-screen Lyrics view with the Queue beside it", async () => {
		renderPlayerBar();
		expect(
			(screen.getByRole("button", { name: "Lyrics" }) as HTMLButtonElement)
				.disabled,
		).toBe(true);

		await act(async () => {
			screen.getByRole("button", { name: "Start track" }).click();
		});
		fireEvent.click(screen.getByRole("button", { name: "Lyrics" }));

		const view = screen.getByRole("dialog", { name: "Lyrics" });
		expect(await within(view).findByText("Line one")).toBeTruthy();
		expect(within(view).getByText("Line two")).toBeTruthy();
		expect(api.getTrackLyrics).toHaveBeenCalledWith(track.id);
		expect(within(view).getByRole("heading", { name: "Queue" })).toBeTruthy();
		expect(within(view).getAllByText("Track 1").length).toBeGreaterThan(0);

		fireEvent.keyDown(document, { key: "Escape" });
		expect(screen.queryByRole("dialog", { name: "Lyrics" })).toBeNull();
	});

	it("mutes from the volume icon and restores the previous level on unmute", async () => {
		renderPlayerBar();

		fireEvent.change(screen.getByLabelText("Volume"), {
			target: { value: "0.4" },
		});
		fireEvent.click(screen.getByRole("button", { name: "Mute" }));
		expect((screen.getByLabelText("Volume") as HTMLInputElement).value).toBe(
			"0",
		);

		fireEvent.click(screen.getByRole("button", { name: "Unmute" }));
		expect((screen.getByLabelText("Volume") as HTMLInputElement).value).toBe(
			"0.4",
		);
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

	it("toggles the active Playback Session from the primary control", async () => {
		renderPlayerBar();

		await act(async () => {
			screen.getByRole("button", { name: "Start track" }).click();
		});
		fireEvent.click(screen.getByRole("button", { name: "Pause" }));

		expect(screen.getByRole("button", { name: "Play" })).toBeTruthy();

		fireEvent.click(screen.getByRole("button", { name: "Play" }));
		expect(screen.getByRole("button", { name: "Pause" })).toBeTruthy();
	});

	it("renders bit depth and sample rate quality details with track actions menu", async () => {
		renderPlayerBar();
		await openActionsMenu();

		expect(screen.getByLabelText("Quality 24-bit · 96 kHz")).toBeTruthy();
		expect(screen.getByText("24-bit · 96 kHz")).toBeTruthy();
		expect(screen.getByText("Add to playlist")).toBeTruthy();
		expect(screen.getByRole("menuitem", { name: "Play next" })).toBeTruthy();
		expect(screen.getByText("Go to album")).toBeTruthy();
		expect(screen.getByText("Go to artist")).toBeTruthy();
		expect(
			(screen.getByRole("menuitem", { name: "Download" }) as HTMLButtonElement)
				.disabled,
		).toBe(true);
		expect(screen.getByRole("menuitem", { name: "Details" })).toBeTruthy();
	});

	it("shows a dash for tracks that carry no bit depth", async () => {
		const bitDepth = track.bitDepth;
		track.bitDepth = 0;
		try {
			renderPlayerBar();
			await openActionsMenu();

			expect(screen.getByLabelText("Quality - · 96 kHz")).toBeTruthy();
		} finally {
			track.bitDepth = bitDepth;
		}
	});

	it("opens compact output modes in the Player Bar and applies Normal", async () => {
		const engine = new InMemoryPlaybackEngine({ outputMode: "direct-alsa" });
		const fallbackToSystemOutput = vi.spyOn(engine, "fallbackToSystemOutput");
		renderPlayerBar(undefined, engine);
		await act(async () => {
			screen.getByRole("button", { name: "Start track" }).click();
		});

		const trigger = screen.getByRole("button", {
			name: "Quality 24-bit · 96 kHz",
		});
		expect(trigger.getAttribute("title")).toBe("Output mode: Exclusive");
		fireEvent.click(trigger);
		const menu = screen.getByRole("menu", { name: "Output mode" });
		expect(within(menu).getAllByRole("menuitemradio")).toHaveLength(3);
		fireEvent.click(
			within(menu).getByRole("menuitemradio", { name: "Normal" }),
		);

		expect(fallbackToSystemOutput).toHaveBeenCalledOnce();
		expect(
			screen
				.getByRole("button", { name: "Quality 24-bit · 96 kHz" })
				.getAttribute("title"),
		).toBe("Output mode: Normal");
	});

	it("shows live radio progress instead of track time", async () => {
		renderPlayerBar();

		await act(async () => {
			screen.getByRole("button", { name: "Start radio" }).click();
		});

		expect(screen.getByText("LIVE")).toBeTruthy();
		expect(screen.queryByText("--:--")).toBeNull();
		expect(screen.queryByText("0:00")).toBeNull();
		expect(screen.queryByLabelText("Seek")).toBeNull();
		expect(screen.getByLabelText("Quality High Quality")).toBeTruthy();
	});

	it("shows an inline error banner with Retry and Skip after a failed Track", async () => {
		const { engine } = renderPlayerBar();
		await act(async () => {
			screen.getByRole("button", { name: "Start track" }).click();
		});
		await act(async () =>
			engine.fail({ code: "playback-failed", message: "Playback failed" }),
		);
		await act(async () => {});

		const banner = screen.getByTestId("playback-error-banner");
		expect(banner.textContent).toContain("Playback failed");
		expect(within(banner).getByRole("button", { name: "Retry" })).toBeTruthy();
		// The test queue holds one item, so there is nothing to skip to.
		expect(within(banner).queryByRole("button", { name: "Skip" })).toBeNull();

		const play = vi.spyOn(engine, "play");
		fireEvent.click(within(banner).getByRole("button", { name: "Retry" }));
		expect(play).toHaveBeenCalledOnce();
	});

	it("changes speed, arms the sleep timer and queues the Track from the actions menu", async () => {
		const { engine } = renderPlayerBar();
		const setPlaybackRate = vi.spyOn(engine, "setPlaybackRate");
		await openActionsMenu();

		fireEvent.click(screen.getByRole("menuitemradio", { name: "1.5×" }));
		expect(setPlaybackRate).toHaveBeenLastCalledWith(1.5);

		fireEvent.click(screen.getByRole("menuitemradio", { name: "After track" }));
		expect(engine.getState().stopAfterCurrent).toBe(true);

		fireEvent.click(screen.getByRole("menuitem", { name: "Add to queue" }));
		await act(async () => {});
		expect(api.appendQueueItem).toHaveBeenCalledWith(
			track.id,
			expect.any(String),
		);
	});

	it("shows the next Queue item as Up next and jumps to it", async () => {
		const nextTrack = { ...track, id: "track-2", title: "Track 2" };
		const twoItemApi: PlaybackApi = {
			...api,
			getQueue: vi.fn(async () => ({
				items: [
					{ id: "item-1", trackId: track.id, position: 0, track },
					{
						id: "item-2",
						trackId: nextTrack.id,
						position: 1,
						track: nextTrack,
					},
				],
				revision: "1",
			})),
		};
		const engine = new InMemoryPlaybackEngine();
		render(
			<LayoutProvider initialPreferences={defaultPreferences}>
				<PlaybackProvider api={twoItemApi} engine={engine}>
					<PlaybackStarter />
					<PlayerBar />
				</PlaybackProvider>
			</LayoutProvider>,
		);
		await act(async () => {});
		await act(async () => {
			screen.getByRole("button", { name: "Start track" }).click();
		});

		const upNext = screen.getByTestId("up-next");
		expect(upNext.textContent).toContain("Track 2");
		await act(async () => {
			fireEvent.click(upNext);
		});
		expect(engine.getState().source).toMatchObject({
			track: { id: "track-2" },
		});
	});

	it("favorites the current saved Radio Station from the Player Bar", async () => {
		const onToggleStationFavorite = vi.fn();
		render(
			<LayoutProvider initialPreferences={defaultPreferences}>
				<PlaybackProvider api={api} engine={new InMemoryPlaybackEngine()}>
					<RadioStarter />
					<PlayerBar
						isCurrentStationFavorite={false}
						onToggleStationFavorite={onToggleStationFavorite}
					/>
				</PlaybackProvider>
			</LayoutProvider>,
		);
		await act(async () => {
			screen.getByRole("button", { name: "Start radio" }).click();
		});

		fireEvent.click(screen.getByRole("button", { name: "Add to favorites" }));
		expect(onToggleStationFavorite).toHaveBeenCalledWith(radioStation.id, true);
	});

	it("announces the new Track to assistive technology", async () => {
		vi.useFakeTimers();
		try {
			renderPlayerBar();
			await act(async () => {
				screen.getByRole("button", { name: "Start track" }).click();
			});
			act(() => {
				vi.advanceTimersByTime(500);
			});
			expect(screen.getByTestId("now-playing-announcer").textContent).toBe(
				"Now playing: Track 1 by Artist",
			);
		} finally {
			vi.useRealTimers();
		}
	});

	it("shows stream details on the quality pill while a Track plays", async () => {
		renderPlayerBar(
			undefined,
			new InMemoryPlaybackEngine({ outputMode: "system" }),
		);
		expect(screen.queryByTestId("quality-details")).toBeNull();

		await act(async () => {
			screen.getByRole("button", { name: "Start track" }).click();
		});

		const details = screen.getByTestId("quality-details");
		expect(within(details).getByText("Output mode")).toBeTruthy();
		expect(within(details).getByText("Normal")).toBeTruthy();
	});

	it("opens the keyboard shortcut help from the bar and with the ? key", () => {
		renderPlayerBar();
		fireEvent.click(screen.getByRole("button", { name: "Keyboard shortcuts" }));
		expect(
			screen.getByRole("dialog", { name: "Keyboard shortcuts" }),
		).toBeTruthy();

		fireEvent.keyDown(document.body, { key: "Escape" });
		expect(
			screen.queryByRole("dialog", { name: "Keyboard shortcuts" }),
		).toBeNull();

		fireEvent.keyDown(document.body, { key: "?" });
		expect(
			screen.getByRole("dialog", { name: "Keyboard shortcuts" }),
		).toBeTruthy();
	});

	it("seeks and changes volume from the arrow keys", async () => {
		const { engine } = renderPlayerBar();
		const seek = vi.spyOn(engine, "seek");
		await act(async () => {
			screen.getByRole("button", { name: "Start track" }).click();
		});

		fireEvent.keyDown(document.body, { key: "ArrowRight" });
		expect(seek).toHaveBeenLastCalledWith(5);
		fireEvent.keyDown(document.body, { key: "ArrowRight", shiftKey: true });
		expect(seek).toHaveBeenLastCalledWith(35);
		fireEvent.keyDown(document.body, { key: "ArrowLeft" });
		expect(seek).toHaveBeenLastCalledWith(30);

		fireEvent.keyDown(document.body, { key: "ArrowDown" });
		expect((screen.getByLabelText("Volume") as HTMLInputElement).value).toBe(
			"0.75",
		);
		fireEvent.keyDown(document.body, { key: "m" });
		expect((screen.getByLabelText("Volume") as HTMLInputElement).value).toBe(
			"0",
		);
		fireEvent.keyDown(document.body, { key: "m" });
		expect((screen.getByLabelText("Volume") as HTMLInputElement).value).toBe(
			"0.75",
		);
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
			await screen.findByRole("menuitem", { name: "Remove from Favorites" }),
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

	it("labels a WAV bitrate as read from the container", async () => {
		const format = track.format;
		track.format = "wav";
		try {
			renderPlayerBar();
			await openActionsMenu();
			fireEvent.click(screen.getByRole("menuitem", { name: "Details" }));

			const dialog = screen.getByRole("dialog", { name: "Track 1" });
			expect(within(dialog).getByText("1411 kbps (Native)")).toBeTruthy();
		} finally {
			track.format = format;
		}
	});

	it("opens a track info modal from the actions menu", async () => {
		renderPlayerBar();
		await openActionsMenu();
		fireEvent.click(screen.getByRole("menuitem", { name: "Details" }));

		expect(screen.getByRole("dialog", { name: "Track 1" })).toBeTruthy();
		const dialog = screen.getByRole("dialog", { name: "Track 1" });
		expect(within(dialog).getByText("Title")).toBeTruthy();
		expect(within(dialog).getByText("Album 1")).toBeTruthy();
		expect(
			within(dialog).getByText("1411 kbps (Calculated by app)"),
		).toBeTruthy();
		expect(within(dialog).getByText("96 kHz")).toBeTruthy();
		expect(within(dialog).getByText("Track ReplayGain")).toBeTruthy();
		expect(within(dialog).getByText("Gain -7.25 dB")).toBeTruthy();
		// Same row set as the track list "Details" dialog (shared builder).
		expect(within(dialog).getByText("Disc")).toBeTruthy();
		expect(within(dialog).queryByText("Album ReplayGain")).toBeNull();
		expect(within(dialog).queryByText("Unavailable")).toBeNull();
		expect(within(dialog).queryByText("Size")).toBeNull();
		expect(within(dialog).queryByText("Id")).toBeNull();
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

	it("routes Previous, Next, and Shuffle through public engine controls", async () => {
		const engine = new InMemoryPlaybackEngine();
		const previous = vi.spyOn(engine, "previous");
		const next = vi.spyOn(engine, "next");
		const toggleShuffle = vi.spyOn(engine, "toggleShuffle");
		renderPlayerBar(undefined, engine);
		await act(async () => {
			screen.getByRole("button", { name: "Start track" }).click();
		});

		fireEvent.click(screen.getByRole("button", { name: "Previous" }));
		fireEvent.click(screen.getByRole("button", { name: "Next" }));
		fireEvent.click(screen.getByRole("button", { name: "Shuffle off" }));

		expect(previous).toHaveBeenCalledOnce();
		expect(next).toHaveBeenCalledOnce();
		expect(toggleShuffle).toHaveBeenCalledOnce();
	});

	it.each([
		["mpv-crash-loop", "Native playback stopped after repeated failures."],
		["mpv-restart-failed", "Native playback could not restart. Try again."],
	] as const)("renders %s as an actionable alert", async (code, message) => {
		const engine = new InMemoryPlaybackEngine();
		renderPlayerBar(undefined, engine);

		await act(async () => engine.fail({ code, message }));

		expect(screen.getByRole("alert").textContent).toContain(message);
	});
});

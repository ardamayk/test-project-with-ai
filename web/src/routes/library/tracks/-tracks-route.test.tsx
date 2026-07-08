import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	act,
	cleanup,
	fireEvent,
	render,
	screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TracksPage } from "./index";

const mocks = vi.hoisted(() => ({
	listTracks: vi.fn(),
	listPlaylists: vi.fn(),
	getPlaylist: vi.fn(),
	addPlaylistTrack: vi.fn(),
	removePlaylistTrack: vi.fn(),
}));

const libraryTracks = [
	{
		id: "t1",
		title: "Anti-Hero",
		artistName: "Taylor Swift",
		albumId: "a1",
		durationMs: 200_000,
		format: "flac",
		genre: "Synthpop",
	},
	{
		id: "t2",
		title: "Bad Blood",
		artistName: "Taylor Swift",
		albumId: "a2",
		durationMs: 211_000,
		format: "flac",
		genre: "Pop",
	},
];

vi.mock("#/lib/api", () => ({
	apiClient: {
		listTracks: mocks.listTracks,
		listPlaylists: mocks.listPlaylists,
		getPlaylist: mocks.getPlaylist,
		addPlaylistTrack: mocks.addPlaylistTrack,
		removePlaylistTrack: mocks.removePlaylistTrack,
	},
}));

vi.mock("#/components/track-list", () => ({
	TrackList: ({
		tracks,
		numbering,
		showFavorite,
		compact,
	}: {
		tracks: Array<{ id: string; title: string }>;
		numbering?: string;
		showFavorite?: boolean;
		compact?: boolean;
	}) => (
		<div>
			<p data-testid="track-numbering">{numbering}</p>
			<p data-testid="track-show-favorite">{String(showFavorite)}</p>
			<p data-testid="track-compact">{String(compact)}</p>
			{tracks.map((track) => (
				<p key={track.id}>{track.title}</p>
			))}
		</div>
	),
}));

function renderWithQuery(ui: React.ReactElement) {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return render(
		<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
	);
}

describe("tracks route", () => {
	beforeEach(() => {
		mocks.listTracks.mockReset();
		mocks.listTracks.mockResolvedValue({
			items: libraryTracks,
		});
		mocks.listPlaylists.mockResolvedValue({
			items: [{ id: "favorites", name: "Favorites", isDefault: true }],
		});
		mocks.getPlaylist.mockResolvedValue({ tracks: [] });
		mocks.addPlaylistTrack.mockResolvedValue({ tracks: [] });
		mocks.removePlaylistTrack.mockResolvedValue({ tracks: [] });
	});

	afterEach(() => {
		vi.useRealTimers();
		cleanup();
	});

	it("keeps track search in the header and debounces API queries", async () => {
		renderWithQuery(<TracksPage />);

		await screen.findByText("Anti-Hero");
		expect(screen.getByTestId("track-numbering").textContent).toBe("list");
		expect(screen.getByTestId("track-show-favorite").textContent).toBe("true");
		expect(screen.getByTestId("track-compact").textContent).toBe("true");
		expect(mocks.listTracks).toHaveBeenCalledTimes(1);
		expect(mocks.listTracks).toHaveBeenLastCalledWith({
			limit: 200,
			q: undefined,
		});

		const input = screen.getByPlaceholderText("Search tracks…");
		const header = input.closest("header");
		expect(header).toBeTruthy();
		expect(header?.className).toContain("sticky");
		expect(header?.className).toContain("top-0");
		expect(screen.getByTestId("tracks-page-shell").className).toContain(
			"overflow-hidden",
		);
		expect(screen.getByTestId("tracks-page-content").className).toContain(
			"[scrollbar-width:none]",
		);
		expect(input.className).toContain("h-11");
		expect(input.className).toContain("pl-10");
		expect(input.parentElement?.className).toContain("sm:max-w-md");

		vi.useFakeTimers();
		fireEvent.change(input, { target: { value: "blue" } });

		await act(async () => {
			vi.advanceTimersByTime(249);
		});
		expect(mocks.listTracks).toHaveBeenCalledTimes(1);

		await act(async () => {
			vi.advanceTimersByTime(1);
			await Promise.resolve();
		});

		expect(mocks.listTracks).toHaveBeenCalledWith({
			limit: 200,
			q: "blue",
		});
	});

	it("filters the current track list immediately while debounced search is pending", async () => {
		renderWithQuery(<TracksPage />);

		await screen.findByText("Anti-Hero");
		const input = screen.getByPlaceholderText("Search tracks…");

		vi.useFakeTimers();
		fireEvent.change(input, { target: { value: "anti-hero" } });

		expect(screen.getByText("Anti-Hero")).toBeTruthy();
		expect(screen.queryByText("Bad Blood")).toBeNull();
		expect(mocks.listTracks).toHaveBeenCalledTimes(1);
	});

	it("keeps filtered local results visible if the debounced search request fails", async () => {
		mocks.listTracks
			.mockResolvedValueOnce({ items: libraryTracks })
			.mockRejectedValueOnce(new Error("backend search failed"));

		renderWithQuery(<TracksPage />);

		await screen.findByText("Anti-Hero");
		const input = screen.getByPlaceholderText("Search tracks…");

		vi.useFakeTimers();
		fireEvent.change(input, { target: { value: "anti-hero" } });
		await act(async () => {
			vi.advanceTimersByTime(250);
			await Promise.resolve();
			await Promise.resolve();
		});

		expect(screen.getByText("Anti-Hero")).toBeTruthy();
		expect(screen.queryByText("Bad Blood")).toBeNull();
		expect(screen.queryByText("Failed to load tracks")).toBeNull();
	});
});

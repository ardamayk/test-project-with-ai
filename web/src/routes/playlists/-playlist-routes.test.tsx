import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PlaylistDetailContent } from "./$playlistId";
import { PlaylistsPage } from "./index";

const mocks = vi.hoisted(() => ({
	listPlaylists: vi.fn(),
	getPlaylist: vi.fn(),
	removePlaylistTrack: vi.fn(),
	playTrack: vi.fn(),
	queueTracks: vi.fn(),
}));

vi.mock("@tanstack/react-router", async () => {
	const actual = await vi.importActual<typeof import("@tanstack/react-router")>(
		"@tanstack/react-router",
	);
	return {
		...actual,
		Link: ({
			children,
			to,
			params,
			...props
		}: {
			children: React.ReactNode;
			to: string;
			params?: Record<string, string>;
		}) => (
			<a
				href={params ? to.replace("$playlistId", params.playlistId) : to}
				{...props}
			>
				{children}
			</a>
		),
	};
});

vi.mock("#/lib/api", () => ({
	apiClient: {
		listPlaylists: mocks.listPlaylists,
		getPlaylist: mocks.getPlaylist,
		getAlbumCoverUrl: (albumId: string) => `/cover/${albumId}`,
		removePlaylistTrack: mocks.removePlaylistTrack,
	},
}));

vi.mock("@repo/ui", () => ({
	AlbumArt: ({
		coverUrl,
		className,
	}: {
		coverUrl?: string | null;
		className?: string;
	}) => (coverUrl ? <img alt="" src={coverUrl} className={className} /> : null),
	usePlayback: () => ({
		playTrack: mocks.playTrack,
		queueTracks: mocks.queueTracks,
		currentTrack: null,
	}),
}));

const tracks = [
	{
		id: "t1",
		title: "Blue Monday",
		artistName: "New Order",
		albumId: "a1",
		durationMs: 180_000,
		format: "flac",
		genre: "Synthpop",
	},
	{
		id: "t2",
		title: "Bizarre Love Triangle",
		artistName: "New Order",
		albumId: "a2",
		durationMs: 240_000,
		format: "flac",
		genre: "Synthpop",
	},
	{
		id: "t3",
		title: "Age of Consent",
		artistName: "New Order",
		albumId: "a3",
		durationMs: 315_000,
		format: "flac",
		genre: "Rock",
	},
	{
		id: "t4",
		title: "Temptation",
		artistName: "New Order",
		albumId: "a4",
		durationMs: 300_000,
		format: "flac",
		genre: "Dance",
	},
];

function renderWithQuery(ui: React.ReactElement) {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	return render(
		<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
	);
}

describe("playlist routes", () => {
	beforeEach(() => {
		mocks.listPlaylists.mockResolvedValue({
			items: [{ id: "p1", name: "Favorites", isDefault: true, trackCount: 4 }],
		});
		mocks.getPlaylist.mockResolvedValue({
			id: "p1",
			name: "Favorites",
			isDefault: true,
			trackCount: 4,
			tracks,
		});
		mocks.removePlaylistTrack.mockResolvedValue({
			id: "p1",
			name: "Favorites",
			isDefault: true,
			trackCount: 0,
			tracks: [],
		});
		mocks.playTrack.mockClear();
		mocks.queueTracks.mockClear();
		mocks.removePlaylistTrack.mockClear();
	});

	afterEach(() => {
		cleanup();
	});

	it("links playlist cards to detail pages without card playback actions", async () => {
		const { container } = renderWithQuery(<PlaylistsPage />);

		const link = await screen.findByRole("link", {
			name: /Favorites 4 tracks/,
		});

		expect(link.getAttribute("href")).toBe("/playlists/p1");
		expect(screen.queryByRole("button", { name: "Play" })).toBeNull();
		expect(screen.queryByRole("button", { name: "Shuffle" })).toBeNull();
		await waitFor(() => {
			expect(container.querySelectorAll("img")).toHaveLength(4);
		});
	});

	it("plays queues and removes tracks from playlist detail", async () => {
		renderWithQuery(<PlaylistDetailContent playlistId="p1" />);

		await screen.findByRole("heading", { name: "Favorites" });

		fireEvent.click(screen.getByRole("button", { name: "Play" }));
		expect(mocks.playTrack).toHaveBeenCalledWith("t1", [
			"t1",
			"t2",
			"t3",
			"t4",
		]);

		fireEvent.click(screen.getByRole("button", { name: "Queue" }));
		expect(mocks.queueTracks).toHaveBeenCalledWith(["t1", "t2", "t3", "t4"]);

		fireEvent.contextMenu(screen.getByRole("row", { name: /Blue Monday/ }));
		fireEvent.click(screen.getByText("Remove from playlist"));

		await waitFor(() => {
			expect(mocks.removePlaylistTrack).toHaveBeenCalledWith("p1", "t1");
		});
	});

	it("filters playlist detail actions to the visible tracks", async () => {
		const { container } = renderWithQuery(
			<PlaylistDetailContent playlistId="p1" />,
		);

		await screen.findByRole("heading", { name: "Favorites" });
		expect(container.querySelectorAll("img")).toHaveLength(4);

		fireEvent.change(screen.getByPlaceholderText("Search Favorites…"), {
			target: { value: "temptation" },
		});

		expect(screen.queryByText("Blue Monday")).toBeNull();
		expect(screen.getByText("Temptation")).toBeTruthy();

		fireEvent.click(screen.getByRole("button", { name: "Play" }));
		expect(mocks.playTrack).toHaveBeenCalledWith("t4", ["t4"]);

		fireEvent.click(screen.getByRole("button", { name: "Queue" }));
		expect(mocks.queueTracks).toHaveBeenCalledWith(["t4"]);
	});
});

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GenresPage } from "./-genres-page";
import { GenreDetailContent } from "./$genre";

const mocks = vi.hoisted(() => ({
	listTracks: vi.fn(),
	listPlaylists: vi.fn(),
	getPlaylist: vi.fn(),
	addPlaylistTrack: vi.fn(),
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
			<a href={params ? to.replace("$genre", params.genre) : to} {...props}>
				{children}
			</a>
		),
	};
});

vi.mock("#/lib/api", () => ({
	apiClient: {
		listTracks: mocks.listTracks,
		listPlaylists: mocks.listPlaylists,
		getPlaylist: mocks.getPlaylist,
		addPlaylistTrack: mocks.addPlaylistTrack,
		removePlaylistTrack: mocks.removePlaylistTrack,
		getAlbumCoverUrl: (albumId: string) => `/cover/${albumId}`,
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
		getAlbumCoverUrl: (albumId: string) => `/cover/${albumId}`,
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
		genre: "Synthpop; Dance",
	},
	{
		id: "t3",
		title: "Age of Consent",
		artistName: "New Order",
		albumId: "a3",
		durationMs: 315_000,
		format: "flac",
		genre: "Synthpop",
	},
	{
		id: "t4",
		title: "Temptation",
		artistName: "New Order",
		albumId: "a4",
		durationMs: 300_000,
		format: "flac",
		genre: "Synthpop",
	},
];

function renderWithQuery(ui: React.ReactElement) {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return render(
		<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
	);
}

describe("genre routes", () => {
	beforeEach(() => {
		mocks.listTracks.mockResolvedValue({ items: tracks });
		mocks.listPlaylists.mockResolvedValue({
			items: [{ id: "favorites", name: "Favorites", isDefault: true }],
		});
		mocks.getPlaylist.mockResolvedValue({ tracks: [] });
		mocks.addPlaylistTrack.mockResolvedValue({ tracks: [] });
		mocks.removePlaylistTrack.mockResolvedValue({ tracks: [] });
		mocks.playTrack.mockClear();
		mocks.queueTracks.mockClear();
	});

	afterEach(() => {
		cleanup();
	});

	it("renders genre cards derived from track metadata", async () => {
		const { container } = renderWithQuery(<GenresPage />);

		const synthpop = await screen.findByRole("link", {
			name: /Synthpop 4 tracks/,
		});
		const dance = screen.getByRole("link", { name: /Dance 1 track/ });

		expect(synthpop.getAttribute("href")).toBe("/library/genres/Synthpop");
		expect(dance.getAttribute("href")).toBe("/library/genres/Dance");
		expect(synthpop.className).toContain("duration-300");
		expect(synthpop.className).toContain("ease-out");
		expect(synthpop.className).toContain("hover:-translate-y-px");
		expect(synthpop.className).toContain("hover:shadow-lg");
		expect(synthpop.className).toContain("focus-visible:ring-2");
		expect(synthpop.className).toContain("motion-reduce:transition-none");
		expect(synthpop.className).not.toContain("hover:-translate-y-1");
		expect(synthpop.className).toContain("aspect-square");
		expect(synthpop.className).toContain("overflow-hidden");
		expect(screen.getByTestId("genres-page-shell").className).toContain(
			"overflow-hidden",
		);
		expect(screen.getByTestId("genres-page-content").className).toContain(
			"[scrollbar-width:none]",
		);
		expect(
			screen
				.getByTestId("genres-page-content")
				.querySelector(".min-\\[1801px\\]\\:max-w-\\[1476px\\]"),
		).toBeTruthy();
		expect(
			screen
				.getByTestId("genres-page-content")
				.querySelector(".xl\\:grid-cols-5"),
		).toBeTruthy();
		expect(
			container.querySelector('[data-testid="playlist-card-cover-stack"]'),
		).toBeTruthy();
		const overlay = container.querySelector("[data-genre-card-overlay]");
		expect(overlay?.className).toContain("px-3");
		expect(overlay?.className).toContain("pt-14");
		expect(overlay?.querySelector("p")?.className).toContain("font-semibold");
		expect(overlay?.querySelector("p")?.className).not.toContain(
			"group-hover:underline",
		);
		await waitFor(() => {
			expect(container.querySelectorAll("img").length).toBeGreaterThanOrEqual(
				4,
			);
		});
	});

	it("scopes detail playback actions to matching genre tracks", async () => {
		renderWithQuery(<GenreDetailContent genre="Synthpop" />);

		await screen.findByRole("heading", { name: "Synthpop" });
		expect(screen.queryByRole("link", { name: /Back to genres/ })).toBeNull();
		expect(screen.getByTestId("genre-detail-content").className).toContain(
			"min-[1801px]:max-w-[1476px]",
		);

		fireEvent.click(screen.getByRole("button", { name: "Play" }));
		expect(mocks.playTrack).toHaveBeenCalledWith("t1", [
			"t1",
			"t2",
			"t3",
			"t4",
		]);

		fireEvent.click(screen.getByRole("button", { name: "Queue" }));
		expect(mocks.queueTracks).toHaveBeenCalledWith(["t1", "t2", "t3", "t4"]);

		expect(screen.getByText("Blue Monday")).toBeTruthy();
		expect(screen.getByText("Bizarre Love Triangle")).toBeTruthy();
		expect(
			document.querySelector('[data-testid="collection-cover-stack"]'),
		).toBeTruthy();
		expect(
			screen.getAllByRole("button", { name: "Add to favorites" }),
		).toHaveLength(4);
		expect(
			screen.getByRole("row", { name: /Blue Monday/ }).querySelector("td")
				?.className,
		).toContain("py-1.5");
	});

	it("filters genre detail actions to the visible tracks", async () => {
		const { container } = renderWithQuery(
			<GenreDetailContent genre="Synthpop" />,
		);

		await screen.findByRole("heading", { name: "Synthpop" });
		expect(screen.getByTestId("genre-track-search")).toBeTruthy();
		expect(screen.getByPlaceholderText("Search Synthpop…").className).toContain(
			"h-11",
		);
		expect(
			container
				.querySelector('[data-testid="collection-cover-stack"]')
				?.querySelectorAll("img"),
		).toHaveLength(4);
		expect(container.querySelectorAll("img")).toHaveLength(8);

		fireEvent.change(screen.getByPlaceholderText("Search Synthpop…"), {
			target: { value: "bizarre" },
		});

		expect(screen.queryByText("Blue Monday")).toBeNull();
		expect(screen.getByText("Bizarre Love Triangle")).toBeTruthy();

		fireEvent.click(screen.getByRole("button", { name: "Play" }));
		expect(mocks.playTrack).toHaveBeenCalledWith("t2", ["t2"]);

		fireEvent.click(screen.getByRole("button", { name: "Queue" }));
		expect(mocks.queueTracks).toHaveBeenCalledWith(["t2"]);
	});
});

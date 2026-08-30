import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AlbumsPage } from "./-albums-page";

const mocks = vi.hoisted(() => ({
	listAlbums: vi.fn(),
	listArtists: vi.fn(),
}));

vi.mock("#/lib/api", () => ({
	apiClient: {
		listAlbums: mocks.listAlbums,
		listArtists: mocks.listArtists,
	},
}));

vi.mock("#/components/album-grid", () => ({
	AlbumGrid: ({ albums }: { albums: Array<{ id: string; title: string }> }) => (
		<div>
			{albums.map((album) => (
				<p key={album.id}>{album.title}</p>
			))}
		</div>
	),
}));

vi.mock("#/components/scan-library-button", () => ({
	ScanLibraryButton: () => <button type="button">Scan library</button>,
}));

function renderWithQuery(ui: React.ReactElement) {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return render(
		<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
	);
}

describe("albums route", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mocks.listArtists.mockResolvedValue({
			items: [{ id: "artist-1", name: "Taylor Swift" }],
		});
		const albumItems = [
			{
				id: "album-1",
				title: "1989",
				artistName: "Taylor Swift",
				genres: ["Pop"],
				trackCount: 2,
			},
		];
		mocks.listAlbums.mockImplementation(({ q }: { q?: string }) =>
			Promise.resolve({ items: q ? [] : albumItems }),
		);
	});

	afterEach(() => {
		cleanup();
	});

	it("uses the shared compact sticky header and hidden page scrollbar", async () => {
		renderWithQuery(<AlbumsPage />);

		await screen.findByText("1989");
		const header = screen
			.getByRole("heading", { name: "Albums" })
			.closest("header");
		const searchInput = screen.getByPlaceholderText("Search albums...");

		expect(header?.className).toContain("sticky");
		expect(header?.className).toContain("top-0");
		expect(header?.className).toContain("pt-7");
		expect(
			header?.querySelector(".min-\\[1801px\\]\\:max-w-\\[1476px\\]"),
		).toBeTruthy();
		expect(searchInput.closest("header")).toBe(header);
		expect(searchInput.className).toContain("h-11");
		expect(searchInput.className).toContain("pl-10");
		expect(screen.getByTestId("albums-page-shell").className).toContain(
			"overflow-hidden",
		);
		expect(screen.getByTestId("albums-page-content").className).toContain(
			"[scrollbar-width:none]",
		);
		expect(screen.getByTestId("albums-page-content").className).toContain(
			"pt-8",
		);
		const collectionContainer = screen
			.getByTestId("albums-page-content")
			.querySelector(".min-\\[1801px\\]\\:max-w-\\[1476px\\]");
		expect(collectionContainer).toBeTruthy();
		expect(screen.getByRole("button", { name: "Filters" })).toBeTruthy();
		expect(screen.queryByRole("button", { name: "Scan library" })).toBeNull();
	});

	it("opens album filters in a drawer without moving search out of the header", async () => {
		renderWithQuery(<AlbumsPage />);

		await screen.findByText("1989");
		const header = screen
			.getByRole("heading", { name: "Albums" })
			.closest("header");
		expect(
			screen.getByPlaceholderText("Search albums...").closest("header"),
		).toBe(header);
		fireEvent.click(screen.getByRole("button", { name: "Filters" }));
		const drawer = await screen.findByRole("dialog", {
			name: "Album filters",
		});

		expect(within(drawer).getByText("Artist")).toBeTruthy();
		expect(within(drawer).getByText("Genre")).toBeTruthy();
		expect(within(drawer).getByText("1 album")).toBeTruthy();
	});

	it("keeps the header visible while showing ten square loading cards", () => {
		const pendingRequest = new Promise(() => undefined);
		mocks.listAlbums.mockReturnValue(pendingRequest);
		mocks.listArtists.mockReturnValue(pendingRequest);

		renderWithQuery(<AlbumsPage />);

		expect(screen.getByRole("heading", { name: "Albums" })).toBeTruthy();
		expect(screen.getByRole("status").textContent).toContain("Loading albums");
		expect(screen.getAllByTestId("collection-card-skeleton")).toHaveLength(10);
	});

	it("retries all album page queries from the shared error panel", async () => {
		mocks.listAlbums.mockRejectedValue(new Error("album request failed"));
		mocks.listArtists.mockRejectedValue(new Error("artist request failed"));

		renderWithQuery(<AlbumsPage />);

		const alert = await screen.findByRole("alert");
		expect(alert.textContent).toContain("Unable to load albums");
		expect(alert.textContent).toContain("Check your connection and try again.");
		fireEvent.click(within(alert).getByRole("button", { name: "Try again" }));

		await waitFor(() => {
			expect(mocks.listAlbums.mock.calls.length).toBeGreaterThanOrEqual(4);
			expect(mocks.listArtists.mock.calls.length).toBeGreaterThanOrEqual(2);
		});
	});

	it("shows an album error while another page query is still pending", async () => {
		mocks.listAlbums.mockRejectedValue(new Error("album request failed"));
		mocks.listArtists.mockReturnValue(new Promise(() => undefined));

		renderWithQuery(<AlbumsPage />);

		const alert = await screen.findByRole("alert");
		expect(alert.textContent).toContain("Unable to load albums");
		const retryButton = within(alert).getByRole("button", {
			name: "Try again",
		});
		expect((retryButton as HTMLButtonElement).disabled).toBe(false);
		expect(screen.queryByRole("status")).toBeNull();
	});

	it("distinguishes an empty library from filters with no matches", async () => {
		mocks.listAlbums.mockResolvedValue({ items: [] });

		const firstRender = renderWithQuery(<AlbumsPage />);

		expect(await screen.findByText("No albums yet")).toBeTruthy();
		expect(screen.getByText("Scan your library to get started.")).toBeTruthy();
		firstRender.unmount();

		mocks.listAlbums.mockResolvedValue({
			items: [
				{
					id: "album-1",
					title: "1989",
					artistName: "Taylor Swift",
					genres: ["Pop"],
					trackCount: 2,
				},
			],
		});
		renderWithQuery(<AlbumsPage />);
		await screen.findByText("1989");
		mocks.listAlbums.mockResolvedValue({ items: [] });
		fireEvent.change(screen.getByPlaceholderText("Search albums..."), {
			target: { value: "unmatched" },
		});

		expect(
			await screen.findByText("No albums match your filters"),
		).toBeTruthy();
		expect(
			screen.getByText("Try adjusting your search or filters."),
		).toBeTruthy();
	});
});

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	cleanup,
	fireEvent,
	render,
	screen,
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
		mocks.listArtists.mockResolvedValue({
			items: [{ id: "artist-1", name: "Taylor Swift" }],
		});
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
		expect(header?.querySelector(".max-w-6xl")).toBeTruthy();
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
		expect(
			screen.getByTestId("albums-page-content").querySelector(".max-w-6xl"),
		).toBeTruthy();
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
});

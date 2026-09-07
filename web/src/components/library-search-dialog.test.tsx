import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	cleanup,
	fireEvent,
	render,
	screen,
	within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LibrarySearchDialog } from "./library-search-dialog";

const mocks = vi.hoisted(() => ({
	listTracks: vi.fn(),
	listAlbums: vi.fn(),
	listArtists: vi.fn(),
	listPlaylists: vi.fn(),
	navigate: vi.fn(),
	playTrack: vi.fn(),
}));

vi.mock("#/lib/api", () => ({
	apiClient: {
		listTracks: mocks.listTracks,
		listAlbums: mocks.listAlbums,
		listArtists: mocks.listArtists,
		listPlaylists: mocks.listPlaylists,
	},
}));
vi.mock("@tanstack/react-router", () => ({
	useNavigate: () => mocks.navigate,
}));
vi.mock("@repo/ui", () => ({
	usePlayback: () => ({ playTrack: mocks.playTrack }),
}));

const nemo = {
	id: "track-nemo",
	title: "Nemo",
	artistName: "Nightwish",
	artists: [{ id: "artist-1", name: "Nightwish", role: "main" }],
	albumId: "album-decades",
	albumTitle: "Decades",
	durationMs: 276_000,
	discNo: 1,
	trackNo: 2,
	genres: [{ id: "g-metal", name: "Symphonic Metal" }],
};

function renderDialog(onOpenChange = vi.fn()) {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return {
		onOpenChange,
		...render(
			<QueryClientProvider client={client}>
				<LibrarySearchDialog open onOpenChange={onOpenChange} />
			</QueryClientProvider>,
		),
	};
}

describe("LibrarySearchDialog", () => {
	beforeEach(() => {
		vi.useFakeTimers({ shouldAdvanceTime: true });
		mocks.listTracks.mockImplementation(async (params?: { q?: string }) =>
			params?.q
				? { items: [nemo] }
				: // The genre source: every track, no query.
					{ items: [nemo, { ...nemo, id: "track-2", title: "Sleeping Sun" }] },
		);
		mocks.listAlbums.mockResolvedValue({
			items: [
				{
					id: "album-decades",
					title: "Decades",
					artistName: "Nightwish",
					albumArtists: [{ id: "artist-1", name: "Nightwish", role: "main" }],
					genreItems: [],
					releaseIdentifiers: [],
					trackCount: 12,
				},
			],
		});
		mocks.listArtists.mockResolvedValue({
			items: [{ id: "artist-1", name: "Nightwish", albumCount: 3 }],
		});
		mocks.listPlaylists.mockResolvedValue({
			items: [
				{ id: "pl-1", name: "Nemo mix", isDefault: false, trackCount: 4 },
				{ id: "pl-2", name: "Chill", isDefault: false, trackCount: 9 },
			],
		});
	});

	afterEach(() => {
		cleanup();
		vi.clearAllMocks();
		vi.useRealTimers();
	});

	it("groups matches under From Your Library by kind, tracks first", async () => {
		renderDialog();
		fireEvent.change(screen.getByRole("combobox"), {
			target: { value: "ne" },
		});

		const section = await screen.findByRole("region", {
			name: "From Your Library",
		});
		// Every source has answered once the playlist match is on screen.
		await within(section).findByText("Nemo mix");
		const labels = within(section)
			.getAllByText(/^(Track|Album|Artist|Genre|Playlist)$/)
			.map((node) => node.textContent);
		expect(labels).toEqual(["Track", "Album", "Artist", "Playlist"]);
		expect(within(section).getByText("Nemo")).toBeTruthy();
		expect(within(section).getByText("Decades")).toBeTruthy();
		expect(within(section).getByText("Nemo mix")).toBeTruthy();
		expect(within(section).queryByText("Chill")).toBeNull();
		expect(mocks.listTracks).toHaveBeenCalledWith({ limit: 5, q: "ne" });
	});

	it("lists the album a matching track belongs to even when no album title matches", async () => {
		mocks.listAlbums.mockResolvedValue({ items: [] });
		renderDialog();
		fireEvent.change(screen.getByRole("combobox"), {
			target: { value: "nemo" },
		});
		await screen.findByText("Nemo");
		expect(await screen.findByText("Decades")).toBeTruthy();
		expect(screen.getByText("Album")).toBeTruthy();
	});

	it("finds genres from track metadata", async () => {
		renderDialog();
		fireEvent.change(screen.getByRole("combobox"), {
			target: { value: "symph" },
		});
		expect(await screen.findByText("Symphonic Metal")).toBeTruthy();
		expect(screen.getByText("2 tracks")).toBeTruthy();
	});

	it("plays a track on Enter and opens an album from its row", async () => {
		const { onOpenChange } = renderDialog();
		const input = screen.getByRole("combobox");
		fireEvent.change(input, { target: { value: "ne" } });
		await screen.findByText("Nemo");
		await screen.findByText("Decades");

		fireEvent.keyDown(input, { key: "Enter" });
		expect(mocks.playTrack).toHaveBeenCalledWith("track-nemo");
		expect(onOpenChange).toHaveBeenCalledWith(false);

		const decades = screen
			.getByText("Decades", { selector: "span" })
			.closest("button");
		expect(decades).toBeTruthy();
		if (decades) fireEvent.click(decades);
		expect(mocks.navigate).toHaveBeenCalledWith({
			to: "/library/$albumId",
			params: { albumId: "album-decades" },
		});
	});

	it("moves the active row with the arrow keys", async () => {
		renderDialog();
		const input = screen.getByRole("combobox");
		fireEvent.change(input, { target: { value: "ne" } });
		await screen.findByText("Nemo");
		await screen.findByText("Decades");

		const optionFor = (title: string) =>
			screen.getByText(title, { selector: "span" }).closest("button");
		expect(optionFor("Nemo")?.getAttribute("aria-selected")).toBe("true");
		fireEvent.keyDown(input, { key: "ArrowDown" });
		expect(optionFor("Decades")?.getAttribute("aria-selected")).toBe("true");
		fireEvent.keyDown(input, { key: "Enter" });
		expect(mocks.navigate).toHaveBeenCalledWith(
			expect.objectContaining({ to: "/library/$albumId" }),
		);
	});

	it("tells the user when nothing matches", async () => {
		mocks.listTracks.mockResolvedValue({ items: [] });
		mocks.listAlbums.mockResolvedValue({ items: [] });
		mocks.listArtists.mockResolvedValue({ items: [] });
		mocks.listPlaylists.mockResolvedValue({ items: [] });
		renderDialog();
		fireEvent.change(screen.getByRole("combobox"), {
			target: { value: "zzz" },
		});
		expect(await screen.findByText(/matches “zzz”/)).toBeTruthy();
	});

	it("opens with Ctrl+K or / and ignores / while typing elsewhere", () => {
		const onOpenChange = vi.fn();
		const client = new QueryClient();
		render(
			<QueryClientProvider client={client}>
				<LibrarySearchDialog open={false} onOpenChange={onOpenChange} />
				<input aria-label="Other field" />
			</QueryClientProvider>,
		);
		fireEvent.keyDown(window, { key: "k", ctrlKey: true });
		expect(onOpenChange).toHaveBeenLastCalledWith(true);
		fireEvent.keyDown(document.body, { key: "/" });
		expect(onOpenChange).toHaveBeenCalledTimes(2);
		fireEvent.keyDown(screen.getByLabelText("Other field"), { key: "/" });
		expect(onOpenChange).toHaveBeenCalledTimes(2);
	});
});

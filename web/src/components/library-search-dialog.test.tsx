import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	act,
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import { useState } from "react";
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

	it("shows failures instead of empty results and retries failed sources", async () => {
		mocks.listTracks.mockRejectedValue(new Error("Track search unavailable"));
		mocks.listAlbums.mockResolvedValue({ items: [] });
		mocks.listArtists.mockResolvedValue({ items: [] });
		mocks.listPlaylists.mockResolvedValue({ items: [] });
		renderDialog();
		fireEvent.change(screen.getByRole("combobox"), { target: { value: "ne" } });
		expect(await screen.findByRole("alert")).toHaveProperty(
			"textContent",
			"Search results could not be loaded.Retry",
		);
		expect(screen.queryByText(/Nothing in your library/)).toBeNull();
		const albumCalls = mocks.listAlbums.mock.calls.length;
		mocks.listTracks.mockResolvedValue({ items: [nemo] });
		fireEvent.click(screen.getByRole("button", { name: "Retry" }));
		await screen.findByText("Nemo");
		await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
		expect(mocks.listAlbums).toHaveBeenCalledTimes(albumCalls);
	});

	it("keeps successful matches visible when another source fails", async () => {
		mocks.listArtists.mockRejectedValue(new Error("Artist search unavailable"));
		renderDialog();
		fireEvent.change(screen.getByRole("combobox"), { target: { value: "ne" } });
		await screen.findByText("Nemo");
		expect(
			await screen.findByText("Some search results could not be loaded."),
		).toBeTruthy();
		expect(screen.queryByText(/Nothing in your library/)).toBeNull();
	});

	it("hides stale options and blocks Enter until the edited query settles", async () => {
		mocks.listTracks.mockImplementation(async (params?: { q?: string }) => ({
			items: params?.q === "ne" ? [nemo] : [],
		}));
		mocks.listAlbums.mockResolvedValue({ items: [] });
		mocks.listArtists.mockResolvedValue({ items: [] });
		mocks.listPlaylists.mockResolvedValue({ items: [] });
		renderDialog();
		const input = screen.getByRole("combobox");
		fireEvent.change(input, { target: { value: "ne" } });
		await screen.findByText("Nemo");
		fireEvent.change(input, { target: { value: "zzz" } });
		expect(screen.queryAllByRole("option")).toHaveLength(0);
		fireEvent.keyDown(input, { key: "Enter" });
		expect(mocks.playTrack).not.toHaveBeenCalled();
		expect(await screen.findByText(/matches “zzz”/)).toBeTruthy();
	});

	it("starts empty after a rapid close and reopen", async () => {
		const client = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		const dialog = (open: boolean) => (
			<QueryClientProvider client={client}>
				<LibrarySearchDialog open={open} onOpenChange={vi.fn()} />
			</QueryClientProvider>
		);
		const { rerender } = render(dialog(true));
		fireEvent.change(screen.getByRole("combobox"), { target: { value: "ne" } });
		await screen.findByText("Nemo");
		rerender(dialog(false));
		rerender(dialog(true));
		expect(screen.getByRole("combobox")).toHaveProperty("value", "");
		expect(screen.queryAllByRole("option")).toHaveLength(0);
		fireEvent.keyDown(screen.getByRole("combobox"), { key: "Enter" });
		expect(mocks.playTrack).not.toHaveBeenCalled();
	});

	it("scrolls the listbox viewport to reveal keyboard selections without moving the page", async () => {
		renderDialog();
		const input = screen.getByRole("combobox");
		fireEvent.change(input, { target: { value: "ne" } });
		await screen.findByText("Nemo mix");
		const listbox = screen.getByRole("listbox");
		const album = screen.getByRole("option", { name: /Decades/ });
		const track = screen.getByRole("option", { name: /^Nemo\s*Nightwish/ });
		vi.spyOn(listbox, "getBoundingClientRect").mockReturnValue({
			top: 100,
			bottom: 200,
		} as DOMRect);
		vi.spyOn(album, "getBoundingClientRect").mockReturnValue({
			top: 220,
			bottom: 260,
		} as DOMRect);
		vi.spyOn(track, "getBoundingClientRect").mockReturnValue({
			top: 40,
			bottom: 80,
		} as DOMRect);
		document.documentElement.scrollTop = 25;
		fireEvent.keyDown(input, { key: "ArrowDown" });
		expect(listbox.scrollTop).toBe(60);
		fireEvent.keyDown(input, { key: "ArrowUp" });
		expect(listbox.scrollTop).toBe(0);
		expect(document.documentElement.scrollTop).toBe(25);
		document.documentElement.scrollTop = 0;
	});

	it.each([
		"button",
		"shortcut",
	])("restores focus to the %s opener after Escape", async (opener) => {
		function SearchHarness() {
			const [open, setOpen] = useState(false);
			return (
				<>
					<button type="button" onClick={() => setOpen(true)}>
						Open search
					</button>
					<LibrarySearchDialog open={open} onOpenChange={setOpen} />
				</>
			);
		}
		const client = new QueryClient();
		render(
			<QueryClientProvider client={client}>
				<SearchHarness />
			</QueryClientProvider>,
		);
		const button = screen.getByRole("button", { name: "Open search" });
		button.focus();
		if (opener === "button") fireEvent.click(button);
		else fireEvent.keyDown(button, { key: "k", ctrlKey: true });
		const input = screen.getByRole("combobox");
		expect(document.activeElement).toBe(input);
		fireEvent.keyDown(input, { key: "Escape" });
		await act(async () => {
			await vi.advanceTimersByTimeAsync(1);
		});
		await waitFor(() => expect(document.activeElement).toBe(button));
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

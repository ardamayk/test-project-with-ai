import {
	cleanup,
	fireEvent,
	render,
	screen,
	within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TrackList } from "./track-list";

const toggleFavorite = vi.fn();
const playTrack = vi.fn();
const deleteTrack = vi.fn();
let favorite = false;

vi.mock("@repo/ui", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@repo/ui")>();
	return {
		...actual,
		usePlayback: () => ({
			playTrack,
			currentTrack: null,
			getAlbumCoverUrl: (albumId: string) => `/cover/${albumId}`,
		}),
	};
});

vi.mock("#/hooks/use-favorite-tracks", () => ({
	useFavoriteTracks: () => ({
		isFavorite: () => favorite,
		toggleFavorite,
	}),
}));

vi.mock("#/hooks/use-delete-library", () => ({
	useDeleteTrack: () => ({ mutate: deleteTrack, isPending: false }),
	confirmDelete: () => true,
}));

const sampleTrack = {
	id: "t1",
	title: "Welcome to New York",
	artistName: "Taylor Swift",
	albumId: "a1",
	albumTitle: "1989",
	trackNo: 1,
	durationMs: 212_000,
	format: "flac",
	genre: "Pop",
	bitDepth: 24,
	sampleRateHz: 96_000,
	sizeBytes: 50_059_000,
	replayGain: {
		trackGainDb: -7.25,
		trackPeak: 0.98,
		albumGainDb: -6.5,
		albumPeak: 1.01,
	},
};

describe("TrackList", () => {
	afterEach(() => {
		cleanup();
	});

	beforeEach(() => {
		favorite = false;
		playTrack.mockClear();
		toggleFavorite.mockClear();
		deleteTrack.mockClear();
	});

	it("renders album tracks with only the title line", () => {
		const { container } = render(
			<TrackList tracks={[sampleTrack]} albumId="a1" showMeta compact />,
		);
		const row = screen.getByRole("row", { name: /Welcome to New York/ });
		const cover = container.querySelector('img[src="/cover/a1"]');

		expect(screen.getByText("Welcome to New York")).toBeTruthy();
		expect(cover).toBeNull();
		expect(row.textContent).not.toContain("Taylor Swift");
		expect(screen.queryByText("Pop · FLAC · 24-bit · 96 kHz")).toBeNull();
		expect(screen.queryByText(/FLAC/)).toBeNull();
	});

	it("renders non-album tracks with the artist line", () => {
		const { container } = render(<TrackList tracks={[sampleTrack]} compact />);
		const cover = container.querySelector('img[src="/cover/a1"]');

		expect(screen.getByText("Welcome to New York")).toBeTruthy();
		expect(cover).toBeTruthy();
		expect(cover?.className).toContain("size-8");
		expect(screen.getByText("Taylor Swift")).toBeTruthy();
	});

	it("uses visible list numbering when requested", () => {
		const secondTrack = {
			...sampleTrack,
			id: "t2",
			title: "Style",
			albumId: "a2",
			trackNo: 1,
		};

		render(<TrackList tracks={[sampleTrack, secondTrack]} numbering="list" />);

		expect(
			screen.getByRole("row", { name: /1 Welcome to New York/ }),
		).toBeTruthy();
		expect(screen.getByRole("row", { name: /2 Style/ })).toBeTruthy();
	});

	it("toggles favorites through the server-backed favorites hook", () => {
		render(
			<TrackList tracks={[sampleTrack]} albumId="a1" showFavorite compact />,
		);

		fireEvent.click(screen.getByRole("button", { name: "Add to favorites" }));

		expect(toggleFavorite).toHaveBeenCalledWith(sampleTrack.id);
	});

	it("renders filled favorite state", () => {
		favorite = true;

		render(
			<TrackList tracks={[sampleTrack]} albumId="a1" showFavorite compact />,
		);

		expect(
			screen.getByRole("button", { name: "Remove from favorites" }),
		).toBeTruthy();
	});

	it("plays with context tracks on double-click when configured", () => {
		const secondTrack = { ...sampleTrack, id: "t2", title: "Style" };

		render(
			<TrackList
				tracks={[sampleTrack, secondTrack]}
				contextTracks={[secondTrack, sampleTrack]}
				playMode="double"
			/>,
		);

		fireEvent.click(screen.getByText("Style"));
		expect(playTrack).not.toHaveBeenCalled();

		fireEvent.doubleClick(screen.getByText("Style"));
		expect(playTrack).toHaveBeenCalledWith("t2", ["t2", "t1"]);
	});

	it("plays the focused row with Enter in double-click mode", () => {
		render(<TrackList tracks={[sampleTrack]} playMode="double" />);

		fireEvent.keyDown(
			screen.getByRole("row", { name: /Welcome to New York/ }),
			{
				key: "Enter",
			},
		);

		expect(playTrack).toHaveBeenCalledWith("t1", ["t1"]);
	});

	it("hides destructive delete when disabled", () => {
		render(<TrackList tracks={[sampleTrack]} showDelete={false} />);

		expect(screen.queryByText("Delete track")).toBeNull();
	});

	it("renders metadata details and delete actions in the context menu", () => {
		render(<TrackList tracks={[sampleTrack]} />);

		fireEvent.contextMenu(
			screen.getByRole("row", { name: /Welcome to New York/ }),
		);
		fireEvent.click(screen.getByText("Details"));

		const dialog = screen.getByRole("dialog", { name: "Welcome to New York" });
		expect(dialog).toBeTruthy();
		expect(dialog.className).toContain("max-w-2xl");
		expect(dialog.className).toContain("bg-popover");
		expect(within(dialog).getByText("Title")).toBeTruthy();
		expect(within(dialog).getByText("Artist")).toBeTruthy();
		expect(within(dialog).getByText("Album")).toBeTruthy();
		expect(within(dialog).getByText("Track")).toBeTruthy();
		expect(within(dialog).getByText("Duration")).toBeTruthy();
		expect(within(dialog).getByText("Codec")).toBeTruthy();
		expect(within(dialog).getByText("Sample rate")).toBeTruthy();
		expect(within(dialog).getByText("Bit depth")).toBeTruthy();
		expect(within(dialog).getByText("Genre")).toBeTruthy();
		expect(within(dialog).getByText("Size")).toBeTruthy();
		expect(within(dialog).getByText("Track ReplayGain")).toBeTruthy();
		expect(within(dialog).getByText("Album ReplayGain")).toBeTruthy();
		expect(
			within(dialog).getByText("Available · Gain -7.25 dB · Peak 0.980000"),
		).toBeTruthy();
		expect(
			within(dialog).getByText("Available · Gain -6.50 dB · Peak 1.010000"),
		).toBeTruthy();
		expect(within(dialog).getByText("Id")).toBeTruthy();
		expect(within(dialog).getByText("Taylor Swift")).toBeTruthy();
		expect(within(dialog).getByText("1989")).toBeTruthy();
		expect(within(dialog).getByText("1")).toBeTruthy();
		expect(within(dialog).getByText("3m 32s")).toBeTruthy();
		expect(within(dialog).getByText("flac")).toBeTruthy();
		expect(within(dialog).getByText("96 kHz")).toBeTruthy();
		expect(within(dialog).getByText("24-bit")).toBeTruthy();
		expect(within(dialog).getByText("Pop")).toBeTruthy();
		expect(within(dialog).getByText("47.74 MiB")).toBeTruthy();
		expect(within(dialog).getByText("t1")).toBeTruthy();
		expect(within(dialog).getByRole("button", { name: "Close" })).toBeTruthy();

		fireEvent.click(screen.getByRole("button", { name: "Close" }));
		fireEvent.contextMenu(
			screen.getByRole("row", { name: /Welcome to New York/ }),
		);
		fireEvent.click(screen.getByText("Delete track"));

		expect(deleteTrack).toHaveBeenCalledWith("t1", expect.any(Object));
	});

	it("shows partial and absent ReplayGain Metadata availability", () => {
		render(
			<TrackList
				tracks={[
					{
						...sampleTrack,
						replayGain: {
							trackGainDb: -4.25,
							trackPeak: null,
							albumGainDb: null,
							albumPeak: null,
						},
					},
				]}
			/>,
		);

		fireEvent.contextMenu(
			screen.getByRole("row", { name: /Welcome to New York/ }),
		);
		fireEvent.click(screen.getByText("Details"));

		const dialog = screen.getByRole("dialog", { name: "Welcome to New York" });
		expect(within(dialog).getByText("Available · Gain -4.25 dB")).toBeTruthy();
		expect(within(dialog).getByText("Unavailable")).toBeTruthy();
	});

	it("renders custom remove and delete actions together when supplied", () => {
		const removeTrack = vi.fn();

		render(
			<TrackList
				tracks={[sampleTrack]}
				onRemoveTrack={removeTrack}
				removeLabel="Remove from playlist"
			/>,
		);

		fireEvent.contextMenu(
			screen.getByRole("row", { name: /Welcome to New York/ }),
		);
		expect(screen.getByText("Delete track")).toBeTruthy();
		fireEvent.click(screen.getByText("Remove from playlist"));

		expect(removeTrack).toHaveBeenCalledWith(sampleTrack);
	});
});

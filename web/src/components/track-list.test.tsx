import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TrackList } from "./track-list";

const toggleFavorite = vi.fn();
const playTrack = vi.fn();
let favorite = false;

vi.mock("@repo/ui", () => ({
	usePlayback: () => ({
		playTrack,
		currentTrack: null,
	}),
}));

vi.mock("#/hooks/use-favorite-tracks", () => ({
	useFavoriteTracks: () => ({
		isFavorite: () => favorite,
		toggleFavorite,
	}),
}));

vi.mock("#/hooks/use-delete-library", () => ({
	useDeleteTrack: () => ({ mutate: vi.fn(), isPending: false }),
	confirmDelete: () => false,
}));

const sampleTrack = {
	id: "t1",
	title: "Welcome to New York",
	artistName: "Taylor Swift",
	albumId: "a1",
	durationMs: 212_000,
	format: "flac",
	genre: "Pop",
	bitDepth: 24,
	sampleRateHz: 96_000,
};

describe("TrackList", () => {
	afterEach(() => {
		cleanup();
	});

	beforeEach(() => {
		favorite = false;
		playTrack.mockClear();
		toggleFavorite.mockClear();
	});

	it("renders compact metadata line", () => {
		render(<TrackList tracks={[sampleTrack]} albumId="a1" showMeta compact />);
		expect(screen.getByText("Welcome to New York")).toBeTruthy();
		expect(screen.getByText("Pop · FLAC · 24-bit · 96 kHz")).toBeTruthy();
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

	it("renders a custom remove action when supplied", () => {
		const removeTrack = vi.fn();

		render(
			<TrackList
				tracks={[sampleTrack]}
				onRemoveTrack={removeTrack}
				removeLabel="Remove from playlist"
				showDelete={false}
			/>,
		);

		fireEvent.contextMenu(
			screen.getByRole("row", { name: /Welcome to New York/ }),
		);
		fireEvent.click(screen.getByText("Remove from playlist"));

		expect(removeTrack).toHaveBeenCalledWith(sampleTrack);
	});
});

import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
	CollectionCoverStrip,
	pickPreviewTracks,
	pickRandomUniqueAlbumTracks,
} from "./collection-cover-strip";

vi.mock("#/lib/api", () => ({
	apiClient: {
		getAlbumCoverUrl: (albumId: string) => `/cover/${albumId}`,
	},
}));

const tracks = [
	{
		id: "t1",
		title: "A",
		albumId: "a1",
		artistName: "Artist",
		durationMs: 1,
		format: "flac",
	},
	{
		id: "t2",
		title: "B",
		albumId: "a2",
		artistName: "Artist",
		durationMs: 1,
		format: "flac",
	},
	{
		id: "t3",
		title: "C",
		albumId: "a3",
		artistName: "Artist",
		durationMs: 1,
		format: "flac",
	},
	{
		id: "t4",
		title: "D",
		albumId: "a4",
		artistName: "Artist",
		durationMs: 1,
		format: "flac",
	},
	{
		id: "t5",
		title: "E",
		albumId: "a5",
		artistName: "Artist",
		durationMs: 1,
		format: "flac",
	},
];

describe("CollectionCoverStrip", () => {
	it("selects a stable four-track preview from a seed", () => {
		const first = pickPreviewTracks(tracks, "playlist-1").map(
			(track) => track.id,
		);
		const second = pickPreviewTracks(tracks, "playlist-1").map(
			(track) => track.id,
		);

		expect(first).toEqual(second);
		expect(first).toHaveLength(4);
		expect(first).not.toEqual(["t1", "t2", "t3", "t4"]);
	});

	it("renders a fit-content row of four album covers", () => {
		const { container } = render(
			<CollectionCoverStrip tracks={tracks} seed="genre-pop" layout="row" />,
		);

		const strip = container.firstElementChild;
		expect(strip?.className).toContain("w-fit");
		expect(strip?.className).toContain("flex");
		expect(container.querySelectorAll("img")).toHaveLength(4);
	});

	it("renders a two-by-two detail cover grid", () => {
		const { container } = render(
			<CollectionCoverStrip tracks={tracks} seed="genre-pop" layout="grid" />,
		);

		const strip = container.firstElementChild;
		expect(strip?.className).toContain("grid");
		expect(strip?.className).toContain("grid-cols-2");
		expect(container.querySelectorAll("img")).toHaveLength(4);
	});

	it("selects at most four random unique album covers for playlist stacks", () => {
		const selected = pickRandomUniqueAlbumTracks(
			[...tracks, { ...tracks[0], id: "t6" }, { ...tracks[1], id: "t7" }],
			4,
			() => 0.9,
		);

		expect(selected).toHaveLength(4);
		expect(new Set(selected.map((track) => track.albumId)).size).toBe(4);
	});
});

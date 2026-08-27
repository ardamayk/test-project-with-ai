import { describe, expect, it } from "vitest";
import { getAlbumGenres } from "./album-genres";

describe("getAlbumGenres", () => {
	it("uses album genres when present", () => {
		const genres = getAlbumGenres({
			id: "a1",
			title: "Album",
			artistId: "ar1",
			artistName: "Artist",
			tracks: [],
			genres: ["Pop", "Rock"],
		});
		expect(genres).toEqual(["Pop", "Rock"]);
	});

	it("falls back to unique track genres", () => {
		const genres = getAlbumGenres({
			id: "a1",
			title: "Album",
			artistId: "ar1",
			artistName: "Artist",
			genres: [],
			tracks: [
				{
					id: "t1",
					title: "A",
					artistName: "Artist",
					albumId: "a1",
					durationMs: 1,
					format: "flac",
					genre: "Pop",
				},
				{
					id: "t2",
					title: "B",
					artistName: "Artist",
					albumId: "a1",
					durationMs: 1,
					format: "flac",
					genre: "pop; Synthpop",
				},
			],
		});
		expect(genres).toEqual(["Pop", "Synthpop"]);
	});
});

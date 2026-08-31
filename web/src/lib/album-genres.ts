import type { AlbumDetail } from "@repo/api-client";
import { getAlbumGenreNames, getTrackGenreNames } from "./library-display";

export function getAlbumGenres(album: AlbumDetail): string[] {
	const albumGenres = getAlbumGenreNames(album);
	if (album.genreItems !== undefined || albumGenres.length > 0) {
		return albumGenres;
	}

	const seen = new Set<string>();
	const out: string[] = [];
	for (const track of album.tracks) {
		for (const genre of getTrackGenreNames(track)) {
			const key = genre.toLowerCase();
			if (seen.has(key)) continue;
			seen.add(key);
			out.push(genre);
		}
	}
	return out.sort((a, b) =>
		a.localeCompare(b, undefined, { sensitivity: "base" }),
	);
}

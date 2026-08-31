import type { Track } from "@repo/api-client";
import { getTrackArtistName, getTrackGenreNames } from "./library-display";

export function filterTracksByText(tracks: Track[], query: string): Track[] {
	const needle = query.trim().toLowerCase();
	if (!needle) return tracks;

	return tracks.filter((track) =>
		[
			track.title,
			getTrackArtistName(track),
			track.albumTitle,
			...getTrackGenreNames(track),
		].some((value) => value?.toLowerCase().includes(needle)),
	);
}

import type { Track } from "@repo/api-client";

export function filterTracksByText(tracks: Track[], query: string): Track[] {
	const needle = query.trim().toLowerCase();
	if (!needle) return tracks;

	return tracks.filter((track) =>
		[track.title, track.artistName, track.albumTitle, track.genre].some(
			(value) => value?.toLowerCase().includes(needle),
		),
	);
}

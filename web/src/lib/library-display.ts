import type { Album, Track } from "@repo/api-client";

const LEGACY_GENRE_DELIMITER_PATTERN = /[;/|,]+/;

export function getTrackArtistName(
	track: Pick<Track, "artistName" | "artists">,
): string {
	return formatArtistCredits(track.artists) ?? track.artistName;
}

export function getAlbumArtistName(
	album: Pick<Album, "artistName" | "albumArtists">,
): string {
	return formatArtistCredits(album.albumArtists) ?? album.artistName;
}

export function getTrackGenreNames(
	track: Pick<Track, "genre" | "genres">,
): string[] {
	if (track.genres !== undefined) {
		return track.genres.map((genre) => genre.name);
	}
	return splitLegacyGenres(track.genre);
}

export function getAlbumGenreNames(
	album: Pick<Album, "genres" | "genreItems">,
): string[] {
	if (album.genreItems !== undefined) {
		return album.genreItems.map((genre) => genre.name);
	}
	return album.genres ?? [];
}

function formatArtistCredits(
	credits?: Array<{ id: string; name: string }>,
): string | null {
	if (!credits || credits.length === 0) return null;
	return credits.map((credit) => credit.name).join(", ");
}

function splitLegacyGenres(value?: string): string[] {
	if (!value) return [];
	const genres: string[] = [];
	const seen = new Set<string>();
	for (const part of value.split(LEGACY_GENRE_DELIMITER_PATTERN)) {
		const genre = part.trim();
		const key = genre.toLocaleLowerCase();
		if (!genre || seen.has(key)) continue;
		seen.add(key);
		genres.push(genre);
	}
	return genres;
}

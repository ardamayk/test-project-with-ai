import type { Album } from "@repo/api-client";

export function sortAlbumsByYear(albums: Album[]): Album[] {
	return [...albums].sort((a, b) => {
		const yearA = a.year ?? -1;
		const yearB = b.year ?? -1;
		if (yearA !== yearB) return yearB - yearA;
		return a.title.localeCompare(b.title, undefined, { sensitivity: "base" });
	});
}

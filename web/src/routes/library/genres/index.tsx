import type { Track } from "@repo/api-client";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useMemo } from "react";
import { CollectionCoverStrip } from "#/components/collection-cover-strip";
import { apiClient } from "#/lib/api";

export const Route = createFileRoute("/library/genres/")({
	component: GenresPage,
});

const genreDelimiterPattern = /[;/|,]+/;

export function splitTrackGenres(value?: string | null): string[] {
	if (!value) return [];
	const seen = new Set<string>();
	const genres: string[] = [];
	for (const part of value.split(genreDelimiterPattern)) {
		const genre = part.trim();
		if (!genre) continue;
		const key = genre.toLowerCase();
		if (seen.has(key)) continue;
		seen.add(key);
		genres.push(genre);
	}
	return genres;
}

export function trackHasGenre(track: Track, genre: string): boolean {
	const target = genre.toLowerCase();
	return splitTrackGenres(track.genre).some(
		(item) => item.toLowerCase() === target,
	);
}

function collectGenres(tracks: Track[]) {
	const byKey = new Map<
		string,
		{ name: string; trackCount: number; tracks: Track[] }
	>();
	for (const track of tracks) {
		for (const genre of splitTrackGenres(track.genre)) {
			const key = genre.toLowerCase();
			const current = byKey.get(key);
			if (current) {
				current.trackCount += 1;
				current.tracks.push(track);
			} else {
				byKey.set(key, { name: genre, trackCount: 1, tracks: [track] });
			}
		}
	}
	return [...byKey.values()].sort((a, b) =>
		a.name.localeCompare(b.name, undefined, { sensitivity: "base" }),
	);
}

export function GenresPage() {
	const tracks = useQuery({
		queryKey: ["library", "tracks", "genres"],
		queryFn: () => apiClient.listTracks({ limit: 500 }),
		staleTime: 60_000,
	});

	const genres = useMemo(
		() => collectGenres(tracks.data?.items ?? []),
		[tracks.data?.items],
	);

	if (tracks.isLoading) {
		return <div className="p-6 text-foreground text-sm">Loading genres…</div>;
	}

	if (tracks.isError) {
		return (
			<div className="p-6 text-destructive text-sm">Failed to load genres</div>
		);
	}

	return (
		<div className="p-6">
			<div className="mb-6">
				<h1 className="font-semibold text-2xl text-heading">Genres</h1>
				<p className="text-foreground text-sm">Browse tracks by genre.</p>
			</div>
			{genres.length === 0 ? (
				<p className="text-foreground text-sm">No tagged genres yet.</p>
			) : (
				<div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
					{genres.map((genre) => (
						<Link
							key={genre.name}
							to="/library/genres/$genre"
							params={{ genre: genre.name }}
							className="flex items-center gap-3 rounded-lg border border-border bg-card/40 p-4"
						>
							<CollectionCoverStrip tracks={genre.tracks} seed={genre.name} />
							<div className="min-w-0">
								<p className="truncate font-medium text-heading">
									{genre.name}
								</p>
								<p className="text-caption text-xs">
									{genre.trackCount} track{genre.trackCount === 1 ? "" : "s"}
								</p>
							</div>
						</Link>
					))}
				</div>
			)}
		</div>
	);
}

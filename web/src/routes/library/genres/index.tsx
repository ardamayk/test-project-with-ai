import type { Track } from "@repo/api-client";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useMemo } from "react";
import { CollectionCoverCardStack } from "#/components/collection-cover-strip";
import { PageHeader, PageShell } from "#/components/page-layout";
import { apiClient } from "#/lib/api";

export const Route = createFileRoute("/library/genres/")({
	component: GenresPage,
});

const GENRES_WIDE_CENTER_CLASS =
	"min-[1801px]:mx-auto min-[1801px]:w-full min-[1801px]:max-w-[1476px]";

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
		<PageShell
			testId="genres-page-shell"
			contentTestId="genres-page-content"
			header={
				<PageHeader
					title="Genres"
					description="Browse tracks by genre."
					innerClassName={GENRES_WIDE_CENTER_CLASS}
				/>
			}
		>
			{genres.length === 0 ? (
				<p className="text-foreground text-sm">No tagged genres yet.</p>
			) : (
				<div
					className={`${GENRES_WIDE_CENTER_CLASS} grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-4 xl:grid-cols-5`}
				>
					{genres.map((genre) => (
						<Link
							key={genre.name}
							to="/library/genres/$genre"
							params={{ genre: genre.name }}
							className="group relative aspect-square overflow-hidden rounded-md border border-border bg-card transition duration-300 ease-out hover:-translate-y-1 hover:bg-muted/50 hover:shadow-lg"
						>
							<CollectionCoverCardStack tracks={genre.tracks} />
							<div
								data-genre-card-overlay
								className="absolute inset-x-0 bottom-0 z-50 bg-gradient-to-t from-background/95 via-background/75 to-transparent p-2 text-foreground"
							>
								<p className="truncate font-medium text-heading text-xs leading-tight group-hover:underline">
									{genre.name}
								</p>
								<p className="truncate text-[11px] text-caption leading-tight">
									{genre.trackCount} track{genre.trackCount === 1 ? "" : "s"}
								</p>
							</div>
						</Link>
					))}
				</div>
			)}
		</PageShell>
	);
}

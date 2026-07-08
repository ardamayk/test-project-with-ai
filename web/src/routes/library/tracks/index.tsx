import type { Track } from "@repo/api-client";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Search } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { PageHeader, PageShell } from "#/components/page-layout";
import { TrackList } from "#/components/track-list";
import { Input } from "#/components/ui/input";
import { apiClient } from "#/lib/api";
import { filterTracksByText } from "#/lib/filter-tracks";

export const Route = createFileRoute("/library/tracks/")({
	component: TracksPage,
});

function useDebouncedValue<T>(value: T, delayMs: number): T {
	const [debounced, setDebounced] = useState(value);

	useEffect(() => {
		const timeout = window.setTimeout(() => setDebounced(value), delayMs);
		return () => window.clearTimeout(timeout);
	}, [value, delayMs]);

	return debounced;
}

export function TracksPage() {
	const [search, setSearch] = useState("");
	const [lastTracks, setLastTracks] = useState<Track[]>([]);
	const debouncedSearch = useDebouncedValue(search.trim(), 250);
	const tracks = useQuery({
		queryKey: ["library", "tracks", debouncedSearch],
		queryFn: () =>
			apiClient.listTracks({ limit: 200, q: debouncedSearch || undefined }),
		placeholderData: (previous) => previous,
	});
	const sourceTracks = tracks.data?.items ?? lastTracks;
	const visibleTracks = useMemo(
		() => filterTracksByText(sourceTracks, search),
		[sourceTracks, search],
	);

	useEffect(() => {
		if (tracks.data?.items) {
			setLastTracks(tracks.data.items);
		}
	}, [tracks.data?.items]);

	return (
		<PageShell
			testId="tracks-page-shell"
			contentTestId="tracks-page-content"
			header={
				<PageHeader
					title="Tracks"
					description="All tracks in your library"
					actions={
						<div className="relative w-full sm:max-w-md">
							<Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-caption" />
							<Input
								className="h-11 rounded-xl bg-[var(--player)] pl-10 text-sm"
								placeholder="Search tracks…"
								value={search}
								onChange={(e) => setSearch(e.target.value)}
							/>
						</div>
					}
				/>
			}
		>
			{tracks.isLoading && sourceTracks.length === 0 ? (
				<p className="text-foreground text-sm">Loading tracks…</p>
			) : tracks.isError && sourceTracks.length === 0 ? (
				<p className="text-destructive text-sm">Failed to load tracks</p>
			) : visibleTracks.length === 0 ? (
				<p className="text-foreground text-sm">No tracks match this search.</p>
			) : (
				<TrackList
					tracks={visibleTracks}
					showFavorite
					showMeta
					compact
					numbering="list"
				/>
			)}
		</PageShell>
	);
}

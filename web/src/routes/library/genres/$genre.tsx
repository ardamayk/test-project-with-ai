import { usePlayback } from "@repo/ui";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Search } from "lucide-react";
import { useMemo } from "react";
import { CollectionDetailHeader } from "#/components/collection-detail-header";
import { TrackList } from "#/components/track-list";
import { Input } from "#/components/ui/input";
import { apiClient } from "#/lib/api";
import {
	formatTrackCollectionDuration,
	useTrackCollectionViewState,
} from "#/lib/track-collection-view-state";
import { trackHasGenre } from "./index";

export const Route = createFileRoute("/library/genres/$genre")({
	component: GenreDetailPage,
});

const GENRE_DETAIL_WIDE_CENTER_CLASS =
	"min-[1801px]:mx-auto min-[1801px]:w-full min-[1801px]:max-w-[1476px]";

function GenreDetailPage() {
	const { genre } = Route.useParams();
	return <GenreDetailContent genre={decodeURIComponent(genre)} />;
}

export function GenreDetailContent({ genre }: { genre: string }) {
	const { playTrack, queueTracks } = usePlayback();
	const tracks = useQuery({
		queryKey: ["library", "tracks", "genre", genre],
		queryFn: () => apiClient.listTracks({ limit: 500 }),
		staleTime: 60_000,
	});

	const genreTracks = useMemo(
		() =>
			(tracks.data?.items ?? []).filter((track) => trackHasGenre(track, genre)),
		[tracks.data?.items, genre],
	);
	const collection = useTrackCollectionViewState(genreTracks, {
		playTrack,
		queueTracks,
	});

	if (tracks.isLoading) {
		return <div className="p-6 text-foreground text-sm">Loading genre…</div>;
	}

	if (tracks.isError) {
		return <div className="p-6 text-destructive text-sm">Genre not found</div>;
	}

	return (
		<div className="p-6">
			<div
				data-testid="genre-detail-content"
				className={GENRE_DETAIL_WIDE_CENTER_CLASS}
			>
				<CollectionDetailHeader
					kind="Genre"
					title={genre}
					subtitle="Library genre"
					metaTags={[
						`${genreTracks.length} track${genreTracks.length === 1 ? "" : "s"}`,
						collection.totalDurationMs > 0
							? formatTrackCollectionDuration(collection.totalDurationMs)
							: "Duration unknown",
					]}
					trackCount={collection.visibleTracks.length}
					coverTracks={genreTracks}
					onPlay={collection.handlePlay}
					onShuffle={collection.handleShuffle}
					onQueue={collection.handleQueue}
				/>

				<section className="mt-6">
					{genreTracks.length > 0 ? (
						<div
							data-testid="genre-track-search"
							className="relative mb-4 w-full"
						>
							<Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-caption" />
							<Input
								className="h-11 rounded-xl bg-[var(--player)] pl-10 text-sm"
								placeholder={`Search ${genre}…`}
								value={collection.search}
								onChange={(event) => collection.setSearch(event.target.value)}
							/>
						</div>
					) : null}
					{genreTracks.length === 0 ? (
						<p className="text-foreground text-sm">
							No tracks found for this genre.
						</p>
					) : collection.visibleTracks.length === 0 ? (
						<p className="text-foreground text-sm">
							No tracks match this search.
						</p>
					) : (
						<TrackList
							tracks={collection.visibleTracks}
							contextTracks={collection.visibleTracks}
							playMode="double"
							showFavorite
							showMeta
							showDelete
							compact
						/>
					)}
				</section>
			</div>
		</div>
	);
}

import { usePlayback } from "@repo/ui";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useMemo } from "react";
import { CollectionDetailHeader } from "#/components/collection-detail-header";
import { TrackList } from "#/components/track-list";
import { apiClient } from "#/lib/api";
import {
	formatTrackCollectionDuration,
	useTrackCollectionViewState,
} from "#/lib/track-collection-view-state";
import { trackHasGenre } from "./index";

export const Route = createFileRoute("/library/genres/$genre")({
	component: GenreDetailPage,
});

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
			<Link
				to="/library/genres"
				className="mb-5 inline-block text-foreground text-sm hover:text-heading"
			>
				← Back to genres
			</Link>

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
				searchValue={collection.search}
				searchPlaceholder={`Search ${genre}…`}
				onSearchChange={collection.setSearch}
				onPlay={collection.handlePlay}
				onShuffle={collection.handleShuffle}
				onQueue={collection.handleQueue}
			/>

			<section className="mt-6">
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
						showMeta
						showDelete
					/>
				)}
			</section>
		</div>
	);
}

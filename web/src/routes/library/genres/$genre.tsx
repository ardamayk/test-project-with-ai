import type { Track } from "@repo/api-client";
import { usePlayback } from "@repo/ui";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { CollectionDetailHeader } from "#/components/collection-detail-header";
import { TrackList } from "#/components/track-list";
import { apiClient } from "#/lib/api";
import { filterTracksByText } from "#/lib/filter-tracks";
import { trackHasGenre } from "./index";

export const Route = createFileRoute("/library/genres/$genre")({
	component: GenreDetailPage,
});

function formatTotalDuration(ms: number): string {
	if (!ms || ms < 0) return "0m";
	const total = Math.floor(ms / 1000);
	const hours = Math.floor(total / 3600);
	const minutes = Math.floor((total % 3600) / 60);
	if (hours > 0) return `${hours}h ${minutes}m`;
	return `${minutes}m`;
}

function shuffleTracks(tracks: Track[]): Track[] {
	const next = [...tracks];
	for (let i = next.length - 1; i > 0; i -= 1) {
		const j = Math.floor(Math.random() * (i + 1));
		[next[i], next[j]] = [next[j], next[i]];
	}
	return next;
}

function GenreDetailPage() {
	const { genre } = Route.useParams();
	return <GenreDetailContent genre={decodeURIComponent(genre)} />;
}

export function GenreDetailContent({ genre }: { genre: string }) {
	const { playTrack, queueTracks } = usePlayback();
	const [search, setSearch] = useState("");
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
	const visibleTracks = useMemo(
		() => filterTracksByText(genreTracks, search),
		[genreTracks, search],
	);
	const visibleTrackIds = visibleTracks.map((track) => track.id);
	const totalDurationMs = genreTracks.reduce(
		(sum, track) => sum + (track.durationMs ?? 0),
		0,
	);

	if (tracks.isLoading) {
		return <div className="p-6 text-foreground text-sm">Loading genre…</div>;
	}

	if (tracks.isError) {
		return <div className="p-6 text-destructive text-sm">Genre not found</div>;
	}

	const handlePlay = () => {
		const first = visibleTracks[0];
		if (!first) return;
		void playTrack(first.id, visibleTrackIds);
	};

	const handleShuffle = () => {
		const shuffled = shuffleTracks(visibleTracks);
		const first = shuffled[0];
		if (!first) return;
		void playTrack(
			first.id,
			shuffled.map((track) => track.id),
		);
	};

	const handleQueue = () => {
		void queueTracks(visibleTrackIds);
	};

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
					totalDurationMs > 0
						? formatTotalDuration(totalDurationMs)
						: "Duration unknown",
				]}
				trackCount={visibleTracks.length}
				coverTracks={genreTracks}
				searchValue={search}
				searchPlaceholder={`Search ${genre}…`}
				onSearchChange={setSearch}
				onPlay={handlePlay}
				onShuffle={handleShuffle}
				onQueue={handleQueue}
			/>

			<section className="mt-6">
				{genreTracks.length === 0 ? (
					<p className="text-foreground text-sm">
						No tracks found for this genre.
					</p>
				) : visibleTracks.length === 0 ? (
					<p className="text-foreground text-sm">
						No tracks match this search.
					</p>
				) : (
					<TrackList
						tracks={visibleTracks}
						contextTracks={visibleTracks}
						playMode="double"
						showMeta
						showDelete
					/>
				)}
			</section>
		</div>
	);
}

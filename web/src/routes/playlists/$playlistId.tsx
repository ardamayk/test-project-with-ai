import type { Track } from "@repo/api-client";
import { usePlayback } from "@repo/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { CollectionDetailHeader } from "#/components/collection-detail-header";
import { TrackList } from "#/components/track-list";
import { apiClient } from "#/lib/api";
import { filterTracksByText } from "#/lib/filter-tracks";

export const Route = createFileRoute("/playlists/$playlistId")({
	component: PlaylistDetailPage,
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

function PlaylistDetailPage() {
	const { playlistId } = Route.useParams();
	return <PlaylistDetailContent playlistId={playlistId} />;
}

export function PlaylistDetailContent({ playlistId }: { playlistId: string }) {
	const queryClient = useQueryClient();
	const { playTrack, queueTracks } = usePlayback();
	const [search, setSearch] = useState("");
	const playlist = useQuery({
		queryKey: ["playlist", playlistId],
		queryFn: () => apiClient.getPlaylist(playlistId),
	});

	const removeTrack = useMutation({
		mutationFn: (trackId: string) =>
			apiClient.removePlaylistTrack(playlistId, trackId),
		onSuccess: async () => {
			await Promise.all([
				queryClient.invalidateQueries({ queryKey: ["playlist", playlistId] }),
				queryClient.invalidateQueries({ queryKey: ["playlists"] }),
			]);
		},
	});

	if (playlist.isLoading) {
		return <div className="p-6 text-foreground text-sm">Loading playlist…</div>;
	}

	if (playlist.isError || !playlist.data) {
		return (
			<div className="p-6 text-destructive text-sm">Playlist not found</div>
		);
	}

	const data = playlist.data;
	const visibleTracks = filterTracksByText(data.tracks, search);
	const visibleTrackIds = visibleTracks.map((track) => track.id);
	const totalDurationMs = data.tracks.reduce(
		(sum, track) => sum + (track.durationMs ?? 0),
		0,
	);
	const trackCount = data.trackCount ?? data.tracks.length;

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
				to="/playlists"
				className="mb-5 inline-block text-foreground text-sm hover:text-heading"
			>
				← Back to playlists
			</Link>

			<CollectionDetailHeader
				kind="Playlist"
				title={data.name}
				subtitle={data.isDefault ? "Default playlist" : "User playlist"}
				metaTags={[
					`${trackCount} track${trackCount === 1 ? "" : "s"}`,
					totalDurationMs > 0
						? formatTotalDuration(totalDurationMs)
						: "Duration unknown",
				]}
				trackCount={visibleTracks.length}
				coverTracks={data.tracks}
				searchValue={search}
				searchPlaceholder={`Search ${data.name}…`}
				onSearchChange={setSearch}
				onPlay={handlePlay}
				onShuffle={handleShuffle}
				onQueue={handleQueue}
			/>

			<section className="mt-6">
				{data.tracks.length === 0 ? (
					<p className="text-foreground text-sm">This playlist is empty.</p>
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
						showDelete={false}
						onRemoveTrack={(track) => removeTrack.mutate(track.id)}
						removeLabel="Remove from playlist"
					/>
				)}
			</section>
		</div>
	);
}

import { usePlayback } from "@repo/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { CollectionDetailHeader } from "#/components/collection-detail-header";
import { TrackList } from "#/components/track-list";
import { apiClient } from "#/lib/api";
import {
	invalidatePlaylistCache,
	playlistQueryKeys,
} from "#/lib/playlist-query-cache";
import {
	formatTrackCollectionDuration,
	useTrackCollectionViewState,
} from "#/lib/track-collection-view-state";

export const Route = createFileRoute("/playlists/$playlistId")({
	component: PlaylistDetailPage,
});

function PlaylistDetailPage() {
	const { playlistId } = Route.useParams();
	return <PlaylistDetailContent playlistId={playlistId} />;
}

export function PlaylistDetailContent({ playlistId }: { playlistId: string }) {
	const queryClient = useQueryClient();
	const { playTrack, queueTracks } = usePlayback();
	const playlist = useQuery({
		queryKey: playlistQueryKeys.detail(playlistId),
		queryFn: () => apiClient.getPlaylist(playlistId),
	});

	const removeTrack = useMutation({
		mutationFn: (trackId: string) =>
			apiClient.removePlaylistTrack(playlistId, trackId),
		onSuccess: async () => {
			await invalidatePlaylistCache(queryClient, playlistId);
		},
	});
	const collection = useTrackCollectionViewState(playlist.data?.tracks ?? [], {
		playTrack,
		queueTracks,
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
	const trackCount = data.trackCount ?? data.tracks.length;

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
					collection.totalDurationMs > 0
						? formatTrackCollectionDuration(collection.totalDurationMs)
						: "Duration unknown",
				]}
				trackCount={collection.visibleTracks.length}
				coverTracks={data.tracks}
				searchValue={collection.search}
				searchPlaceholder={`Search ${data.name}…`}
				onSearchChange={collection.setSearch}
				onPlay={collection.handlePlay}
				onShuffle={collection.handleShuffle}
				onQueue={collection.handleQueue}
			/>

			<section className="mt-6">
				{data.tracks.length === 0 ? (
					<p className="text-foreground text-sm">This playlist is empty.</p>
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
						showDelete={false}
						onRemoveTrack={(track) => removeTrack.mutate(track.id)}
						removeLabel="Remove from playlist"
					/>
				)}
			</section>
		</div>
	);
}

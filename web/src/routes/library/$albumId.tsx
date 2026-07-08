import { usePlayback } from "@repo/ui";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { AlbumDetailHeader } from "#/components/album-detail-header";
import { MoreFromArtist } from "#/components/more-from-artist";
import { TrackList } from "#/components/track-list";
import { apiClient } from "#/lib/api";

export const Route = createFileRoute("/library/$albumId")({
	component: AlbumDetailPage,
});

function AlbumDetailPage() {
	const { albumId } = Route.useParams();
	return <AlbumDetailContent albumId={albumId} />;
}

export function AlbumDetailContent({ albumId }: { albumId: string }) {
	const { playTrack, queueTracks } = usePlayback();
	const album = useQuery({
		queryKey: ["library", "album", albumId],
		queryFn: () => apiClient.getAlbum(albumId),
		staleTime: 0,
	});

	if (album.isLoading) {
		return <div className="p-6 text-foreground text-sm">Loading album…</div>;
	}

	if (album.isError || !album.data) {
		return <div className="p-6 text-destructive text-sm">Album not found</div>;
	}

	const data = album.data;

	const handlePlayAlbum = () => {
		const first = data.tracks[0];
		if (!first) return;
		void playTrack(
			first.id,
			data.tracks.map((track) => track.id),
		);
	};

	const handleQueueAlbum = () => {
		void queueTracks(data.tracks.map((track) => track.id));
	};

	return (
		<div className="p-6">
			<AlbumDetailHeader
				album={data}
				onPlayAlbum={handlePlayAlbum}
				onQueueAlbum={handleQueueAlbum}
			/>

			<section className="mt-6">
				<TrackList
					tracks={data.tracks}
					albumId={data.id}
					showFavorite
					showMeta
					compact
				/>
			</section>

			<MoreFromArtist
				artistId={data.artistId}
				artistName={data.artistName}
				excludeAlbumId={data.id}
			/>
		</div>
	);
}

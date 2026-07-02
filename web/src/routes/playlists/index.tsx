import type { Playlist } from "@repo/api-client";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { CollectionCoverStrip } from "#/components/collection-cover-strip";
import { apiClient } from "#/lib/api";

export const Route = createFileRoute("/playlists/")({
	component: PlaylistsPage,
});

export function PlaylistsPage() {
	const playlists = useQuery({
		queryKey: ["playlists"],
		queryFn: () => apiClient.listPlaylists(),
	});

	if (playlists.isLoading) {
		return (
			<div className="p-6 text-foreground text-sm">Loading playlists…</div>
		);
	}

	if (playlists.isError) {
		return (
			<div className="p-6 text-destructive text-sm">
				Failed to load playlists
			</div>
		);
	}

	return (
		<div className="p-6">
			<div className="mb-6">
				<h1 className="font-semibold text-2xl text-heading">Playlists</h1>
				<p className="text-foreground text-sm">
					Favorites is your default playlist.
				</p>
			</div>
			<div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
				{(playlists.data?.items ?? []).map((playlist) => (
					<PlaylistCard key={playlist.id} playlist={playlist} />
				))}
			</div>
		</div>
	);
}

function PlaylistCard({ playlist }: { playlist: Playlist }) {
	const preview = useQuery({
		queryKey: ["playlist", playlist.id, "preview"],
		queryFn: () => apiClient.getPlaylist(playlist.id),
		enabled: playlist.trackCount > 0,
		staleTime: 60_000,
	});
	const tracks = preview.data?.tracks ?? [];

	return (
		<Link
			to="/playlists/$playlistId"
			params={{ playlistId: playlist.id }}
			className="flex items-center gap-3 rounded-lg border border-border bg-card/40 p-4"
		>
			{tracks.length > 0 ? (
				<CollectionCoverStrip tracks={tracks} seed={playlist.id} />
			) : null}
			<div className="min-w-0">
				<p className="truncate font-medium text-heading">{playlist.name}</p>
				<p className="text-caption text-xs">
					{playlist.trackCount} track{playlist.trackCount === 1 ? "" : "s"}
				</p>
			</div>
		</Link>
	);
}

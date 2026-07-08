import type { Playlist } from "@repo/api-client";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { CollectionCoverRowStack } from "#/components/collection-cover-strip";
import { PageHeader, PageShell } from "#/components/page-layout";
import { apiClient } from "#/lib/api";
import {
	PLAYLIST_PREVIEW_STALE_TIME_MS,
	playlistQueryKeys,
} from "#/lib/playlist-query-cache";

export const Route = createFileRoute("/playlists/")({
	component: PlaylistsPage,
});

export function PlaylistsPage() {
	const playlists = useQuery({
		queryKey: playlistQueryKeys.list,
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
		<PageShell
			testId="playlists-page-shell"
			contentTestId="playlists-page-content"
			header={
				<PageHeader
					title="Playlists"
					description="Favorites is your default playlist."
				/>
			}
		>
			<div className="flex flex-col gap-1">
				{(playlists.data?.items ?? []).map((playlist) => (
					<PlaylistCard key={playlist.id} playlist={playlist} />
				))}
			</div>
		</PageShell>
	);
}

function PlaylistCard({ playlist }: { playlist: Playlist }) {
	const preview = useQuery({
		queryKey: playlistQueryKeys.preview(playlist.id),
		queryFn: () => apiClient.getPlaylist(playlist.id),
		enabled: playlist.trackCount > 0,
		staleTime: PLAYLIST_PREVIEW_STALE_TIME_MS,
	});
	const tracks = preview.data?.tracks ?? [];

	return (
		<Link
			to="/playlists/$playlistId"
			params={{ playlistId: playlist.id }}
			className="flex h-36 items-center gap-4 rounded-md border border-border bg-card/40 px-4 transition-colors hover:bg-muted/50"
		>
			{tracks.length > 0 ? <CollectionCoverRowStack tracks={tracks} /> : null}
			<div className="min-w-0 flex-1">
				<p className="truncate font-medium text-heading">{playlist.name}</p>
				<p className="text-caption text-xs">
					{playlist.trackCount} track{playlist.trackCount === 1 ? "" : "s"}
				</p>
			</div>
		</Link>
	);
}

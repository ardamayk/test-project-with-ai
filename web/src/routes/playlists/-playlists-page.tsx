import type { Playlist } from "@repo/api-client";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CollectionCoverCardStack } from "#/components/collection-cover-strip";
import { PageHeader, PageShell } from "#/components/page-layout";
import { apiClient } from "#/lib/api";
import {
	PLAYLIST_PREVIEW_STALE_TIME_MS,
	playlistQueryKeys,
} from "#/lib/playlist-query-cache";

const PLAYLISTS_WIDE_CENTER_CLASS =
	"min-[1801px]:mx-auto min-[1801px]:w-full min-[1801px]:max-w-[1476px]";

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
					innerClassName={PLAYLISTS_WIDE_CENTER_CLASS}
				/>
			}
		>
			<div
				className={`${PLAYLISTS_WIDE_CENTER_CLASS} grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-4 xl:grid-cols-5`}
			>
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
			className="group relative aspect-square overflow-hidden rounded-md border border-border bg-card transition duration-300 ease-out hover:-translate-y-1 hover:bg-muted/50 hover:shadow-lg"
		>
			{tracks.length > 0 ? <CollectionCoverCardStack tracks={tracks} /> : null}
			<div
				data-playlist-card-overlay
				className="absolute inset-x-0 bottom-0 z-50 bg-gradient-to-t from-background/95 via-background/75 to-transparent p-2 text-foreground"
			>
				<p className="truncate font-medium text-heading text-xs leading-tight group-hover:underline">
					{playlist.name}
				</p>
				<p className="truncate text-[11px] text-caption leading-tight">
					{playlist.trackCount} track{playlist.trackCount === 1 ? "" : "s"}
				</p>
			</div>
		</Link>
	);
}

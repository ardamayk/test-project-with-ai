import {
	AppShell,
	defaultPreferences,
	LayoutProvider,
	type PlaybackApi,
	PlaybackProvider,
	PlayerBar,
	SidebarNav,
} from "@repo/ui";
import type { QueryClient } from "@tanstack/react-query";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";
import { ThemeSync } from "#/components/theme-sync";
import { useLibraryScanSync } from "#/hooks/use-library-scan-sync";
import { apiClient } from "#/lib/api";
import { invalidatePlaylistCache } from "#/lib/playlist-query-cache";

const playbackApi: PlaybackApi = {
	getQueue: () => apiClient.getPlaybackQueue(),
	replaceQueue: (trackIds) => apiClient.replacePlaybackQueue(trackIds),
	appendQueueItem: (trackId) => apiClient.appendPlaybackQueueItem(trackId),
	removeQueueItem: (itemId) => apiClient.removePlaybackQueueItem(itemId),
	getStreamUrl: (trackId) => apiClient.getTrackStreamUrl(trackId),
	getAlbumCoverUrl: (albumId) => apiClient.getAlbumCoverUrl(albumId),
	getRadioStationStreamUrl: (stationId) =>
		apiClient.getRadioStationStreamUrl(stationId),
	getRadioCatalogPreviewStreamUrl: (stationUuid) =>
		apiClient.getRadioCatalogPreviewStreamUrl(stationUuid),
	getRadioNowPlaying: (stationId) => apiClient.getRadioNowPlaying(stationId),
	listPlaylists: () => apiClient.listPlaylists(),
	getPlaylist: (playlistId) => apiClient.getPlaylist(playlistId),
	createPlaylist: (name) => apiClient.createPlaylist({ name }),
	addPlaylistTrack: (playlistId, trackId) =>
		apiClient.addPlaylistTrack(playlistId, trackId),
	removePlaylistTrack: (playlistId, trackId) =>
		apiClient.removePlaylistTrack(playlistId, trackId),
};

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()(
	{
		component: RootLayout,
	},
);

function PlayerBarWithSync() {
	const queryClient = useQueryClient();
	return (
		<PlayerBar
			onPlaylistMutated={() => {
				void invalidatePlaylistCache(queryClient);
			}}
		/>
	);
}

function RootLayout() {
	const queryClient = useQueryClient();
	useLibraryScanSync();
	const preferences = useQuery({
		queryKey: ["preferences"],
		queryFn: () => apiClient.getPreferences(),
	});

	const patchPreferences = useMutation({
		mutationFn: apiClient.patchPreferences,
		onSuccess: (data) => {
			queryClient.setQueryData(["preferences"], data);
		},
	});

	if (preferences.isLoading) {
		return <div className="p-8">Loading…</div>;
	}

	const initial = preferences.data ?? defaultPreferences;

	return (
		<LayoutProvider
			initialPreferences={initial}
			onPreferencesChange={(prefs) => {
				patchPreferences.mutate(prefs);
			}}
		>
			<div className="flex h-full min-h-0 flex-1 flex-col overflow-hidden">
				<ThemeSync />
				<PlaybackProvider api={playbackApi}>
					<AppShell sidebar={<SidebarNav />} bottom={<PlayerBarWithSync />}>
						<Outlet />
					</AppShell>
				</PlaybackProvider>
			</div>
		</LayoutProvider>
	);
}

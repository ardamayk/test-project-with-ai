import { usePlayback } from "@repo/ui";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "#/lib/api";
import { invalidateLibraryCache } from "#/lib/invalidate-library-cache";
import { invalidatePlaylistCache } from "#/lib/playlist-query-cache";

async function syncPlaybackAfterDelete(
	playback: ReturnType<typeof usePlayback>,
	trackId?: string,
) {
	await playback.refreshQueue();

	if (trackId && playback.currentTrack?.id === trackId) {
		await playback.clearQueue();
	}
}

export function useDeleteTrack() {
	const queryClient = useQueryClient();
	const playback = usePlayback();

	return useMutation({
		mutationFn: ({
			trackId,
			confirmationToken,
		}: {
			trackId: string;
			confirmationToken: string;
		}) => apiClient.deleteTrack(trackId, confirmationToken),
		onSuccess: async (_result, { trackId }) => {
			await syncPlaybackAfterDelete(playback, trackId);
			await invalidateLibraryCache(queryClient, { trackId });
			await invalidatePlaylistCache(queryClient);
		},
	});
}

export function usePreviewTrackDeletion() {
	return useMutation({
		mutationFn: (trackId: string) => apiClient.previewTrackDeletion(trackId),
	});
}

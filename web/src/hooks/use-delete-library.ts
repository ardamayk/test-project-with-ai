import type { AlbumDeletionResult } from "@repo/api-client";
import { toast, usePlayback } from "@repo/ui";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
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

export function usePreviewAlbumDeletion() {
	return useMutation({
		mutationFn: (albumId: string) => apiClient.previewAlbumDeletion(albumId),
	});
}

function describeAlbumDeletion(result: AlbumDeletionResult): string {
	const deleted = `${result.deleted.length} track${result.deleted.length === 1 ? "" : "s"} deleted`;
	const failure = result.failed[0];
	if (!failure) return deleted;
	return `${deleted}; stopped at "${failure.trackTitle}": ${failure.reason}`;
}

/**
 * Deletes an Album as one Permanent Track Deletion per Track. The result is
 * per Track, so a partial run is reported as such rather than as an error:
 * the tracks already gone stay gone.
 */
export function useDeleteAlbum() {
	const queryClient = useQueryClient();
	const navigate = useNavigate();
	const playback = usePlayback();

	return useMutation({
		mutationFn: ({
			albumId,
			confirmationToken,
		}: {
			albumId: string;
			confirmationToken: string;
		}) => apiClient.deleteAlbum(albumId, confirmationToken),
		onSuccess: async (result, { albumId }) => {
			const deletedIds = new Set(result.deleted.map((track) => track.trackId));
			if (playback.currentTrack && deletedIds.has(playback.currentTrack.id)) {
				await playback.clearQueue();
			} else {
				await playback.refreshQueue();
			}
			const albumGone = result.failed.length === 0;
			await invalidateLibraryCache(
				queryClient,
				albumGone ? { albumId } : { trackId: result.deleted[0]?.trackId },
			);
			await invalidatePlaylistCache(queryClient);
			if (albumGone) {
				toast.success("Album deleted", {
					description: describeAlbumDeletion(result),
				});
				if (window.location.pathname.includes(albumId)) {
					void navigate({ to: "/library/albums" });
				}
			} else {
				toast.error("Album deletion stopped", {
					description: describeAlbumDeletion(result),
				});
			}
		},
		onError: (cause) => {
			toast.error("Album could not be deleted", { description: cause.message });
		},
	});
}

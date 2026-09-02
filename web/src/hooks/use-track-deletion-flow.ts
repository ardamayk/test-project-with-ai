import type { Track, TrackDeletionPreview } from "@repo/api-client";
import { useState } from "react";
import { useDeleteTrack, usePreviewTrackDeletion } from "./use-delete-library";

export function useTrackDeletionFlow(onDeleted?: (track: Track) => void) {
	const deleteTrack = useDeleteTrack();
	const previewTrackDeletion = usePreviewTrackDeletion();
	const [track, setTrack] = useState<Track | null>(null);
	const [preview, setPreview] = useState<TrackDeletionPreview | null>(null);
	const [error, setError] = useState<string | null>(null);

	const reset = () => {
		setTrack(null);
		setPreview(null);
		setError(null);
	};
	const open = (selectedTrack: Track) => {
		setTrack(selectedTrack);
		setPreview(null);
		setError(null);
		previewTrackDeletion.mutate(selectedTrack.id, {
			onSuccess: setPreview,
			onError: (cause) => setError(cause.message),
		});
	};
	const cancel = () => {
		if (!deleteTrack.isPending) reset();
	};
	const confirm = () => {
		if (!track || !preview) return;
		deleteTrack.mutate(
			{ trackId: track.id, confirmationToken: preview.confirmationToken },
			{
				onSuccess: () => {
					onDeleted?.(track);
					reset();
				},
				onError: (cause) => setError(cause.message),
			},
		);
	};

	return {
		track,
		preview,
		error,
		isLoading: previewTrackDeletion.isPending,
		isDeleting: deleteTrack.isPending,
		open,
		cancel,
		confirm,
	};
}

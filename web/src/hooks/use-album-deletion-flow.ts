import type { Album, AlbumDeletionPreview } from "@repo/api-client";
import { useState } from "react";
import { useDeleteAlbum, usePreviewAlbumDeletion } from "./use-delete-library";

type AlbumSummary = Pick<Album, "id" | "title">;

/**
 * Album deletion mirrors the per-Track flow: open loads the album-level
 * preview, confirm sends its token. Failures surface as toasts from
 * useDeleteAlbum; the dialog only shows preview errors inline.
 */
export function useAlbumDeletionFlow(
	onDeleted?: (album: AlbumSummary) => void,
) {
	const deleteAlbum = useDeleteAlbum();
	const previewAlbumDeletion = usePreviewAlbumDeletion();
	const [album, setAlbum] = useState<AlbumSummary | null>(null);
	const [preview, setPreview] = useState<AlbumDeletionPreview | null>(null);
	const [error, setError] = useState<string | null>(null);

	const reset = () => {
		setAlbum(null);
		setPreview(null);
		setError(null);
	};
	const open = (selectedAlbum: AlbumSummary) => {
		setAlbum(selectedAlbum);
		setPreview(null);
		setError(null);
		previewAlbumDeletion.mutate(selectedAlbum.id, {
			onSuccess: setPreview,
			onError: (cause) => setError(cause.message),
		});
	};
	const cancel = () => {
		if (!deleteAlbum.isPending) reset();
	};
	const confirm = () => {
		if (!album || !preview) return;
		deleteAlbum.mutate(
			{ albumId: album.id, confirmationToken: preview.confirmationToken },
			{
				onSuccess: () => {
					onDeleted?.(album);
					reset();
				},
				onError: (cause) => setError(cause.message),
			},
		);
	};

	return {
		album,
		preview,
		error,
		isLoading: previewAlbumDeletion.isPending,
		isDeleting: deleteAlbum.isPending,
		open,
		cancel,
		confirm,
	};
}

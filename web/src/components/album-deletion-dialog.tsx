import type { Album, AlbumDeletionPreview } from "@repo/api-client";
import { Dialog as DialogPrimitive } from "radix-ui";
import {
	DELETION_DIALOG_CONTENT_CLASS,
	DELETION_DIALOG_OVERLAY_CLASS,
	DeletionActions,
	DeletionRows,
	formatDeletionBytes,
} from "#/components/track-deletion-dialog";

export function AlbumDeletionDialog({
	album,
	preview,
	error,
	isLoading,
	isDeleting,
	onCancel,
	onConfirm,
}: {
	album: Pick<Album, "id" | "title"> | null;
	preview: AlbumDeletionPreview | null;
	error: string | null;
	isLoading: boolean;
	isDeleting: boolean;
	onCancel: () => void;
	onConfirm: () => void;
}) {
	return (
		<DialogPrimitive.Root
			open={Boolean(album)}
			onOpenChange={(open) => !open && onCancel()}
		>
			<DialogPrimitive.Portal>
				<DialogPrimitive.Overlay className={DELETION_DIALOG_OVERLAY_CLASS} />
				<DialogPrimitive.Content className={DELETION_DIALOG_CONTENT_CLASS}>
					<DialogPrimitive.Title className="font-semibold text-heading text-xl">
						Permanently delete every track of {album?.title}?
					</DialogPrimitive.Title>
					<DialogPrimitive.Description className="mt-2 text-caption text-sm">
						Each track and its managed file is deleted one by one. This cannot
						be undone; if a track fails, the ones already deleted stay deleted.
					</DialogPrimitive.Description>
					{isLoading ? (
						<p className="mt-4 text-sm">Loading deletion details…</p>
					) : null}
					{preview ? <AlbumDeletionPreviewDetails preview={preview} /> : null}
					{error ? (
						<p role="alert" className="mt-4 text-destructive text-sm">
							{error}
						</p>
					) : null}
					<DeletionActions
						isReady={Boolean(preview)}
						isDeleting={isDeleting}
						onCancel={onCancel}
						onConfirm={onConfirm}
					/>
				</DialogPrimitive.Content>
			</DialogPrimitive.Portal>
		</DialogPrimitive.Root>
	);
}

function AlbumDeletionPreviewDetails({
	preview,
}: {
	preview: AlbumDeletionPreview;
}) {
	const queueCount = preview.queueReferences.reduce(
		(total, queue) => total + queue.itemCount,
		0,
	);
	return (
		<DeletionRows
			rows={[
				["Album", preview.albumTitle],
				[
					"Tracks",
					`${preview.trackCount} track${preview.trackCount === 1 ? "" : "s"}`,
				],
				["Total size", formatDeletionBytes(preview.totalSizeBytes)],
				[
					"Playlists",
					preview.playlistReferences
						.map((playlist) => playlist.name)
						.join(", ") || "None",
				],
				[
					"Queues",
					queueCount === 0
						? "None"
						: `${queueCount} Queue item${queueCount === 1 ? "" : "s"}`,
				],
			]}
		/>
	);
}

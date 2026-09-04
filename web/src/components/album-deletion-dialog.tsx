import type { Album, AlbumDeletionPreview } from "@repo/api-client";
import { Dialog as DialogPrimitive } from "radix-ui";

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
				<DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-background/70" />
				<DialogPrimitive.Content className="-translate-x-1/2 -translate-y-1/2 fixed top-1/2 left-1/2 z-50 w-[calc(100vw-2rem)] max-w-lg rounded-lg border border-border bg-popover p-5 text-popover-foreground shadow-xl outline-none">
					<DialogPrimitive.Title className="font-semibold text-heading text-xl">
						Permanently delete {album?.title}?
					</DialogPrimitive.Title>
					<DialogPrimitive.Description className="mt-2 text-caption text-sm">
						Every track and its managed file is deleted one by one. This cannot
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
					<div className="mt-6 flex justify-end gap-2">
						<button
							type="button"
							className="rounded-md border border-border px-4 py-2 text-sm"
							disabled={isDeleting}
							onClick={onCancel}
						>
							Cancel
						</button>
						<button
							type="button"
							className="rounded-md bg-destructive px-4 py-2 text-destructive-foreground text-sm"
							disabled={!preview || isDeleting}
							onClick={onConfirm}
						>
							{isDeleting ? "Deleting…" : "Delete permanently"}
						</button>
					</div>
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
	const rows = [
		["Album", preview.albumTitle],
		[
			"Tracks",
			`${preview.trackCount} track${preview.trackCount === 1 ? "" : "s"}`,
		],
		["Total size", `${(preview.totalSizeBytes / 1024 / 1024).toFixed(2)} MiB`],
		[
			"Playlists",
			preview.playlistReferences.map((playlist) => playlist.name).join(", ") ||
				"None",
		],
		[
			"Queues",
			queueCount === 0
				? "None"
				: `${queueCount} Queue item${queueCount === 1 ? "" : "s"}`,
		],
	];
	return (
		<dl className="mt-4 space-y-3 text-sm">
			{rows.map(([label, value]) => (
				<div key={label} className="grid grid-cols-[7rem_minmax(0,1fr)] gap-3">
					<dt className="text-caption">{label}</dt>
					<dd className="break-all text-foreground">{value}</dd>
				</div>
			))}
		</dl>
	);
}

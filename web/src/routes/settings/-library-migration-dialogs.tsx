import type { LibraryMigrationCleanupPreview } from "@repo/api-client";
import { Dialog as DialogPrimitive } from "radix-ui";
import type { ReactNode } from "react";
import { formatMigrationBytes } from "./-library-migration-format";

const DIALOG_CONTENT_CLASS =
	"-translate-x-1/2 -translate-y-1/2 fixed top-1/2 left-1/2 z-50 w-[calc(100vw-2rem)] max-w-lg rounded-lg border border-border bg-popover p-5 text-popover-foreground shadow-xl outline-none";

function MigrationDialogFrame({
	isOpen,
	onCancel,
	onCloseAutoFocus,
	title,
	description,
	children,
}: {
	isOpen: boolean;
	onCancel: () => void;
	onCloseAutoFocus?: (event: Event) => void;
	title: string;
	description: string;
	children: ReactNode;
}) {
	return (
		<DialogPrimitive.Root
			open={isOpen}
			onOpenChange={(open) => !open && onCancel()}
		>
			<DialogPrimitive.Portal>
				<DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-background/70" />
				<DialogPrimitive.Content
					onCloseAutoFocus={onCloseAutoFocus}
					className={DIALOG_CONTENT_CLASS}
				>
					<DialogPrimitive.Title className="font-semibold text-heading text-xl">
						{title}
					</DialogPrimitive.Title>
					<DialogPrimitive.Description className="mt-2 text-caption text-sm">
						{description}
					</DialogPrimitive.Description>
					{children}
				</DialogPrimitive.Content>
			</DialogPrimitive.Portal>
		</DialogPrimitive.Root>
	);
}

/**
 * Cutover activates verified copies under new Track IDs and drops old
 * Playlist, Queue, and snapshot references, as decided for Library Migration.
 * The consequence is stated before the user confirms.
 */
export function MigrationCutoverDialog({
	isOpen,
	verifiedCount,
	error,
	isCuttingOver,
	onCancel,
	onConfirm,
	onCloseAutoFocus,
}: {
	isOpen: boolean;
	verifiedCount: number;
	error: string | null;
	isCuttingOver: boolean;
	onCancel: () => void;
	onConfirm: () => void;
	onCloseAutoFocus?: (event: Event) => void;
}) {
	return (
		<MigrationDialogFrame
			isOpen={isOpen}
			onCancel={onCancel}
			onCloseAutoFocus={onCloseAutoFocus}
			title="Activate migrated Tracks?"
			description="Every verified copy becomes a new Managed Track with a new Track ID. The Legacy Track is removed from the library, and its old Playlist, Queue, and snapshot references are dropped rather than remapped. Legacy source files stay untouched."
		>
			<dl className="mt-4 space-y-3 text-sm">
				<div className="grid grid-cols-[9rem_minmax(0,1fr)] gap-3">
					<dt className="text-caption">Verified copies</dt>
					<dd className="text-foreground">{verifiedCount}</dd>
				</div>
			</dl>
			{error ? (
				<p role="alert" className="mt-4 text-destructive text-sm">
					{error}
				</p>
			) : null}
			<div className="mt-6 flex justify-end gap-2">
				<button
					type="button"
					className="rounded-md border border-border px-4 py-2 text-sm"
					disabled={isCuttingOver}
					onClick={onCancel}
				>
					Cancel
				</button>
				<button
					type="button"
					className="rounded-md bg-primary px-4 py-2 text-primary-foreground text-sm"
					disabled={isCuttingOver || verifiedCount === 0}
					onClick={onConfirm}
				>
					{isCuttingOver ? "Activating…" : "Activate migrated Tracks"}
				</button>
			</div>
		</MigrationDialogFrame>
	);
}

/**
 * Legacy Source Cleanup is irreversible and never implied by a successful
 * migration, so it gets its own destructive confirmation that repeats the
 * exact file count and total size the Music Server will re-verify.
 */
export function LegacySourceCleanupDialog({
	preview,
	error,
	isCleaning,
	onCancel,
	onConfirm,
	onCloseAutoFocus,
}: {
	preview: LibraryMigrationCleanupPreview | null;
	error: string | null;
	isCleaning: boolean;
	onCancel: () => void;
	onConfirm: () => void;
	onCloseAutoFocus?: (event: Event) => void;
}) {
	const eligibleCount = preview?.eligibleCount ?? 0;
	const rows: Array<[string, string]> = [
		["Files to delete", String(eligibleCount)],
		["Total size", formatMigrationBytes(preview?.totalSizeBytes ?? 0)],
		["Kept sources", String(preview?.ineligibleCount ?? 0)],
	];
	return (
		<MigrationDialogFrame
			isOpen={Boolean(preview)}
			onCancel={onCancel}
			onCloseAutoFocus={onCloseAutoFocus}
			title="Permanently delete legacy source files?"
			description="This cannot be undone. Only source files whose bytes were verified as active migrated Managed Tracks are deleted; every target is checked again before removal, and emptied folders are pruned upward to the configured music path."
		>
			<dl className="mt-4 space-y-3 text-sm">
				{rows.map(([label, value]) => (
					<div
						key={label}
						className="grid grid-cols-[9rem_minmax(0,1fr)] gap-3"
					>
						<dt className="text-caption">{label}</dt>
						<dd className="text-foreground">{value}</dd>
					</div>
				))}
			</dl>
			{eligibleCount === 0 ? (
				<p className="mt-4 text-sm">
					No legacy source file is eligible for cleanup.
				</p>
			) : null}
			{error ? (
				<p role="alert" className="mt-4 text-destructive text-sm">
					{error}
				</p>
			) : null}
			<div className="mt-6 flex justify-end gap-2">
				<button
					type="button"
					className="rounded-md border border-border px-4 py-2 text-sm"
					disabled={isCleaning}
					onClick={onCancel}
				>
					Cancel
				</button>
				<button
					type="button"
					className="rounded-md bg-destructive px-4 py-2 text-destructive-foreground text-sm"
					disabled={eligibleCount === 0 || isCleaning}
					onClick={onConfirm}
				>
					{isCleaning ? "Deleting…" : "Delete legacy sources permanently"}
				</button>
			</div>
		</MigrationDialogFrame>
	);
}

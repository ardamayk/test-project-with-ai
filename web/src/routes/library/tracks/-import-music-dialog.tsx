import type { ManagedImportPreview } from "@repo/api-client";
import { X } from "lucide-react";
import { Dialog as DialogPrimitive } from "radix-ui";
import { useState } from "react";
import { Button } from "#/components/ui/button";
import { apiClient } from "#/lib/api";

type ImportState = "idle" | "uploading" | "confirming";

export function ImportMusicDialog({
	isOpen,
	onOpenChange,
	onCommitted,
}: {
	isOpen: boolean;
	onOpenChange: (isOpen: boolean) => void;
	onCommitted: () => Promise<void>;
}) {
	const workflow = useManagedImportWorkflow({ onOpenChange, onCommitted });

	return (
		<DialogPrimitive.Root
			open={isOpen}
			onOpenChange={workflow.handleOpenChange}
		>
			<DialogPrimitive.Portal>
				<DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-background/70 backdrop-blur-sm" />
				<DialogPrimitive.Content
					aria-describedby="import-music-description"
					className="-translate-x-1/2 -translate-y-1/2 fixed top-1/2 left-1/2 z-50 grid max-h-[85vh] w-[calc(100vw-2rem)] max-w-lg gap-5 overflow-y-auto rounded-xl border border-border bg-background p-6 shadow-xl outline-none"
				>
					<ImportDialogHeader isBusy={workflow.isBusy} />
					<ImportFilePicker
						isBusy={workflow.isBusy}
						onFile={workflow.handleFile}
					/>
					<ImportActivity
						importState={workflow.importState}
						errorMessage={workflow.errorMessage}
					/>
					{workflow.preview ? (
						<ImportPreview preview={workflow.preview} />
					) : null}
					<ImportDialogFooter
						canConfirm={Boolean(workflow.preview) && !workflow.isBusy}
						isBusy={workflow.isBusy}
						onCancel={() => workflow.handleOpenChange(false)}
						onConfirm={workflow.handleConfirm}
					/>
				</DialogPrimitive.Content>
			</DialogPrimitive.Portal>
		</DialogPrimitive.Root>
	);
}

function useManagedImportWorkflow({
	onOpenChange,
	onCommitted,
}: {
	onOpenChange: (isOpen: boolean) => void;
	onCommitted: () => Promise<void>;
}) {
	const [importState, setImportState] = useState<ImportState>("idle");
	const [jobId, setJobId] = useState("");
	const [preview, setPreview] = useState<ManagedImportPreview>();
	const [errorMessage, setErrorMessage] = useState("");
	const isBusy = importState !== "idle";

	async function handleFile(file: File | undefined) {
		if (!file) return;
		setImportState("uploading");
		setErrorMessage("");
		setPreview(undefined);
		try {
			const job = await apiClient.createManagedImportJob();
			const nextPreview = await apiClient.uploadManagedImportFile(
				job.id,
				file.name,
				file,
			);
			setJobId(job.id);
			setPreview(nextPreview);
		} catch (error) {
			setErrorMessage(importErrorMessage(error));
		} finally {
			setImportState("idle");
		}
	}

	async function handleConfirm() {
		if (!preview || !jobId) return;
		setImportState("confirming");
		setErrorMessage("");
		try {
			await apiClient.confirmManagedImport(jobId, preview.revision);
			await onCommitted();
			resetDialog();
			onOpenChange(false);
		} catch (error) {
			setErrorMessage(importErrorMessage(error));
		} finally {
			setImportState("idle");
		}
	}

	function resetDialog() {
		setJobId("");
		setPreview(undefined);
		setErrorMessage("");
	}

	function handleOpenChange(nextIsOpen: boolean) {
		if (isBusy) return;
		if (!nextIsOpen) resetDialog();
		onOpenChange(nextIsOpen);
	}

	return {
		importState,
		preview,
		errorMessage,
		isBusy,
		handleFile,
		handleConfirm,
		handleOpenChange,
	};
}

function ImportDialogHeader({ isBusy }: { isBusy: boolean }) {
	return (
		<div className="flex items-start justify-between gap-4">
			<div>
				<DialogPrimitive.Title className="font-semibold text-heading text-xl">
					Import Music
				</DialogPrimitive.Title>
				<DialogPrimitive.Description
					id="import-music-description"
					className="mt-1 text-caption text-sm"
				>
					Upload one strict FLAC, review its metadata, then confirm the Managed
					Track.
				</DialogPrimitive.Description>
			</div>
			<DialogPrimitive.Close asChild>
				<Button type="button" variant="ghost" size="icon" disabled={isBusy}>
					<X className="size-4" />
					<span className="sr-only">Close Import Music</span>
				</Button>
			</DialogPrimitive.Close>
		</div>
	);
}

function ImportFilePicker({
	isBusy,
	onFile,
}: {
	isBusy: boolean;
	onFile: (file: File | undefined) => Promise<void>;
}) {
	return (
		<div className="grid gap-2">
			<label htmlFor="managed-import-file" className="font-medium text-sm">
				FLAC file
			</label>
			<input
				id="managed-import-file"
				type="file"
				accept=".flac,audio/flac"
				disabled={isBusy}
				className="rounded-lg border border-input bg-background px-3 py-2 text-sm file:mr-3 file:rounded-md file:border-0 file:bg-secondary file:px-3 file:py-1.5 file:text-secondary-foreground"
				onChange={(event) => void onFile(event.target.files?.[0])}
			/>
		</div>
	);
}

function ImportActivity({
	importState,
	errorMessage,
}: {
	importState: ImportState;
	errorMessage: string;
}) {
	return (
		<>
			<p aria-live="polite" className="text-caption text-sm">
				{importState === "uploading" ? "Uploading and validating FLAC…" : null}
				{importState === "confirming" ? "Committing Managed Track…" : null}
			</p>
			{errorMessage ? (
				<p role="alert" className="text-destructive text-sm">
					{errorMessage}
				</p>
			) : null}
		</>
	);
}

function ImportDialogFooter({
	canConfirm,
	isBusy,
	onCancel,
	onConfirm,
}: {
	canConfirm: boolean;
	isBusy: boolean;
	onCancel: () => void;
	onConfirm: () => Promise<void>;
}) {
	return (
		<div className="flex justify-end gap-2 border-border border-t pt-4">
			<Button
				type="button"
				variant="outline"
				disabled={isBusy}
				onClick={onCancel}
			>
				Cancel
			</Button>
			<Button
				type="button"
				disabled={!canConfirm}
				onClick={() => void onConfirm()}
			>
				Confirm Import
			</Button>
		</div>
	);
}

function ImportPreview({ preview }: { preview: ManagedImportPreview }) {
	return (
		<section
			aria-label="Import Preview"
			className="grid gap-3 rounded-lg border border-border bg-muted/30 p-4"
		>
			<h3 className="font-semibold text-heading">Import Preview</h3>
			<div>
				<p className="font-medium text-heading">{preview.file.title}</p>
				<p className="text-caption text-sm">
					{preview.file.artists.join(", ")}
				</p>
			</div>
			<dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
				<dt className="text-caption">Album</dt>
				<dd>{preview.file.album}</dd>
				<dt className="text-caption">Album Artist</dt>
				<dd>{preview.file.albumArtists.join(", ")}</dd>
				<dt className="text-caption">Position</dt>
				<dd>
					Disc {preview.file.discNo}, Track {preview.file.trackNo}
				</dd>
				<dt className="text-caption">Genre</dt>
				<dd>{preview.file.genres.join(", ")}</dd>
			</dl>
		</section>
	);
}

function importErrorMessage(error: unknown): string {
	if (error instanceof Error && error.message.trim()) return error.message;
	return "Managed Import failed. Please try again.";
}

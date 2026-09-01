import type {
	ManagedImportBatch,
	ManagedImportBatchFile,
	ManagedImportPreview,
} from "@repo/api-client";
import { X } from "lucide-react";
import { Dialog as DialogPrimitive } from "radix-ui";
import { useState } from "react";
import { Button } from "#/components/ui/button";
import { apiClient } from "#/lib/api";

type ImportState = "idle" | "uploading" | "confirming";

type ImportFileEntry = {
	key: string;
	file: File;
	jobId?: string;
	progress: number;
	state: "accepted" | "rejected" | "unresolved" | "completed";
	selected: boolean;
	preview?: ManagedImportPreview;
	errorMessage?: string;
	outcome?: ManagedImportBatchFile["outcome"];
};

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
					className="-translate-x-1/2 -translate-y-1/2 fixed top-1/2 left-1/2 z-50 grid max-h-[85vh] w-[calc(100vw-2rem)] max-w-2xl gap-5 overflow-y-auto rounded-xl border border-border bg-background p-6 shadow-xl outline-none"
				>
					<ImportDialogHeader isBusy={workflow.isBusy} />
					<ImportFilePicker
						isBusy={workflow.isBusy}
						onFiles={workflow.handleFiles}
					/>
					<ImportActivity
						importState={workflow.importState}
						errorMessage={workflow.errorMessage}
					/>
					<ImportFileList
						entries={workflow.entries}
						onSelectionChange={workflow.handleSelectionChange}
					/>
					<ImportDialogFooter
						canConfirm={workflow.canConfirm}
						isBusy={workflow.isBusy}
						isCompleted={workflow.isCompleted}
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
	const [batch, setBatch] = useState<ManagedImportBatch>();
	const [entries, setEntries] = useState<ImportFileEntry[]>([]);
	const [errorMessage, setErrorMessage] = useState("");
	const isBusy = importState !== "idle";
	const isCompleted = batch?.status === "completed";
	const canConfirm = Boolean(
		batch &&
			entries.length > 0 &&
			!isBusy &&
			!isCompleted &&
			entries.every((entry) => entry.state !== "unresolved"),
	);

	async function handleFiles(fileList: FileList | File[]) {
		const files = Array.from(fileList);
		if (files.length === 0) return;
		setImportState("uploading");
		setErrorMessage("");
		const initialEntries = files.map(createImportFileEntry);
		setEntries(initialEntries);
		try {
			const createdBatch = await apiClient.createManagedImportBatch();
			setBatch(createdBatch);
			const preparedEntries: Array<{
				entry: ImportFileEntry;
				jobId: string;
			}> = [];
			for (const entry of initialEntries) {
				try {
					const job = await apiClient.createManagedImportJob(createdBatch.id);
					updateEntry(entry.key, { jobId: job.id });
					preparedEntries.push({ entry, jobId: job.id });
				} catch (error) {
					updateEntry(entry.key, {
						state: "rejected",
						errorMessage: importErrorMessage(error),
					});
				}
			}
			await Promise.all(
				preparedEntries.map(({ entry, jobId }) => uploadFile(jobId, entry)),
			);
			const previewBatch = await apiClient.getManagedImportBatch(
				createdBatch.id,
			);
			setBatch(previewBatch);
			setEntries((current) => mergeBatchFiles(current, previewBatch.files));
		} catch (error) {
			setErrorMessage(importErrorMessage(error));
		} finally {
			setImportState("idle");
		}
	}

	async function uploadFile(jobId: string, entry: ImportFileEntry) {
		try {
			const preview = await apiClient.uploadManagedImportFile(
				jobId,
				entry.file.name,
				entry.file,
				(progress) => updateEntry(entry.key, { progress }),
			);
			updateEntry(entry.key, {
				state: "accepted",
				selected: true,
				preview,
				progress: 100,
			});
		} catch (error) {
			updateEntry(entry.key, {
				state: "rejected",
				selected: false,
				errorMessage: importErrorMessage(error),
			});
		}
	}

	function updateEntry(key: string, patch: Partial<ImportFileEntry>) {
		setEntries((current) =>
			current.map((entry) =>
				entry.key === key ? { ...entry, ...patch } : entry,
			),
		);
	}

	function handleSelectionChange(key: string, selected: boolean) {
		updateEntry(key, { selected });
	}

	async function handleConfirm() {
		if (!batch || !canConfirm) return;
		setImportState("confirming");
		setErrorMessage("");
		try {
			const selectedFileIds = entries.flatMap((entry) =>
				entry.selected && entry.jobId ? [entry.jobId] : [],
			);
			const report = await apiClient.confirmManagedImportBatch(
				batch.id,
				batch.revision,
				selectedFileIds,
			);
			setBatch(report);
			setEntries((current) => mergeBatchFiles(current, report.files));
			if (
				report.files.some(
					(file) => file.outcome === "imported" || file.outcome === "replaced",
				)
			) {
				await onCommitted();
			}
		} catch (error) {
			setErrorMessage(importErrorMessage(error));
		} finally {
			setImportState("idle");
		}
	}

	function resetDialog() {
		setBatch(undefined);
		setEntries([]);
		setErrorMessage("");
	}

	function handleOpenChange(nextIsOpen: boolean) {
		if (isBusy) return;
		if (!nextIsOpen) resetDialog();
		onOpenChange(nextIsOpen);
	}

	return {
		importState,
		entries,
		errorMessage,
		isBusy,
		isCompleted,
		canConfirm,
		handleFiles,
		handleConfirm,
		handleSelectionChange,
		handleOpenChange,
	};
}

function createImportFileEntry(file: File, index: number): ImportFileEntry {
	return {
		key: `${index}:${file.name}:${file.size}`,
		file,
		progress: 0,
		state: "unresolved",
		selected: false,
	};
}

function mergeBatchFiles(
	entries: ImportFileEntry[],
	files: ManagedImportBatchFile[],
): ImportFileEntry[] {
	return entries.map((entry) => {
		const result = files.find((file) => file.jobId === entry.jobId);
		if (!result) return entry;
		return {
			...entry,
			state: result.state,
			selected: result.selected,
			preview: result.preview ?? entry.preview,
			progress: result.validationProgress,
			errorMessage:
				result.errorReason ?? result.errorCode ?? entry.errorMessage,
			outcome: result.outcome,
		};
	});
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
					Upload audio files, review each result, then confirm selected Tracks.
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
	onFiles,
}: {
	isBusy: boolean;
	onFiles: (files: FileList) => Promise<void>;
}) {
	return (
		<div className="grid gap-2">
			<label htmlFor="managed-import-files" className="font-medium text-sm">
				Audio files
			</label>
			<input
				id="managed-import-files"
				type="file"
				multiple
				accept=".flac,.mp3,.m4a,.ogg,.opus,.wav,audio/*"
				disabled={isBusy}
				className="rounded-lg border border-input bg-background px-3 py-2 text-sm file:mr-3 file:rounded-md file:border-0 file:bg-secondary file:px-3 file:py-1.5 file:text-secondary-foreground"
				onChange={(event) =>
					event.target.files && void onFiles(event.target.files)
				}
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
				{importState === "uploading" ? "Uploading and validating files…" : null}
				{importState === "confirming" ? "Committing selected Tracks…" : null}
			</p>
			{errorMessage ? (
				<p role="alert" className="text-destructive text-sm">
					{errorMessage}
				</p>
			) : null}
		</>
	);
}

function ImportFileList({
	entries,
	onSelectionChange,
}: {
	entries: ImportFileEntry[];
	onSelectionChange: (key: string, selected: boolean) => void;
}) {
	if (entries.length === 0) return null;
	return (
		<section aria-label="Import Preview" className="grid gap-3">
			<h3 className="font-semibold text-heading">Import Preview</h3>
			{entries.map((entry) => (
				<ImportFileRow
					key={entry.key}
					entry={entry}
					onSelectionChange={onSelectionChange}
				/>
			))}
		</section>
	);
}

function ImportFileRow({
	entry,
	onSelectionChange,
}: {
	entry: ImportFileEntry;
	onSelectionChange: (key: string, selected: boolean) => void;
}) {
	const filename = entry.preview?.file.originalFilename ?? entry.file.name;
	return (
		<article className="grid gap-2 rounded-lg border border-border bg-muted/30 p-4">
			<div className="flex items-start justify-between gap-3">
				<label className="flex min-w-0 items-start gap-3">
					<input
						type="checkbox"
						aria-label={`Select ${filename}`}
						checked={entry.selected}
						disabled={entry.state !== "accepted"}
						onChange={(event) =>
							onSelectionChange(entry.key, event.target.checked)
						}
					/>
					<span className="min-w-0">
						<span className="block truncate font-medium text-heading">
							{entry.preview?.file.title ?? filename}
						</span>
						{entry.preview ? (
							<span className="block text-caption text-sm">
								<span>{entry.preview.file.artists.join(", ")}</span>
								{" · "}
								<span>{entry.preview.file.album}</span>
							</span>
						) : null}
					</span>
				</label>
				<span className="rounded-full bg-secondary px-2 py-1 font-medium text-xs">
					{entry.outcome
						? outcomeLabel(entry.outcome)
						: stateLabel(entry.state)}
				</span>
			</div>
			{entry.state === "unresolved" ? (
				<div
					className="h-2 overflow-hidden rounded-full bg-secondary"
					role="progressbar"
					aria-label={`${filename} upload progress`}
					aria-valuenow={entry.progress}
					aria-valuemin={0}
					aria-valuemax={100}
				>
					<div
						className="h-full bg-primary transition-[width]"
						style={{ width: `${entry.progress}%` }}
					/>
				</div>
			) : null}
			{entry.errorMessage ? (
				<p className="text-destructive text-sm">{entry.errorMessage}</p>
			) : null}
		</article>
	);
}

function ImportDialogFooter({
	canConfirm,
	isBusy,
	isCompleted,
	onCancel,
	onConfirm,
}: {
	canConfirm: boolean;
	isBusy: boolean;
	isCompleted: boolean;
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
				{isCompleted ? "Done" : "Cancel"}
			</Button>
			{!isCompleted ? (
				<Button
					type="button"
					disabled={!canConfirm}
					onClick={() => void onConfirm()}
				>
					Confirm Import
				</Button>
			) : null}
		</div>
	);
}

function stateLabel(state: ImportFileEntry["state"]): string {
	return {
		accepted: "Accepted",
		rejected: "Rejected",
		unresolved: "Unresolved",
		completed: "Completed",
	}[state];
}

function outcomeLabel(
	outcome: NonNullable<ImportFileEntry["outcome"]>,
): string {
	return {
		imported: "Imported",
		rejected: "Rejected",
		failed: "Failed",
		replaced: "Replaced",
		not_attempted: "Not attempted",
	}[outcome];
}

function importErrorMessage(error: unknown): string {
	if (error instanceof Error && error.message.trim()) return error.message;
	return "Managed Import failed. Please try again.";
}

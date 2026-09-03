import { X } from "lucide-react";
import { Dialog as DialogPrimitive } from "radix-ui";
import { Button } from "#/components/ui/button";
import { isDesktopClient } from "#/desktop/bridge";
import {
	type DuplicateDecision,
	type ImportFileEntry,
	type ImportState,
	SUPPORTED_AUDIO_FILE_ACCEPT,
	useManagedImportWorkflow,
} from "./-managed-import-workflow";

export function ImportMusicDialog({
	isOpen,
	onOpenChange,
	onCommitted,
	onCloseAutoFocus,
}: {
	isOpen: boolean;
	onOpenChange: (isOpen: boolean) => void;
	onCommitted: () => Promise<void>;
	/** Restores focus to the opener; the dialog has no DialogTrigger. */
	onCloseAutoFocus?: (event: Event) => void;
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
					onCloseAutoFocus={onCloseAutoFocus}
					className="-translate-x-1/2 -translate-y-1/2 fixed top-1/2 left-1/2 z-50 grid max-h-[85vh] w-[calc(100vw-2rem)] max-w-2xl gap-5 overflow-y-auto rounded-xl border border-border bg-background p-6 shadow-xl outline-none"
				>
					<ImportDialogHeader isBusy={workflow.isCloseLocked} />
					<ImportFilePicker
						isBusy={workflow.isPickerLocked}
						onFiles={workflow.handleFiles}
						onDesktopSelection={workflow.handleDesktopSelection}
					/>
					<ImportActivity
						importState={workflow.importState}
						errorMessage={workflow.errorMessage}
					/>
					<ImportFileList
						entries={workflow.entries}
						isBusy={workflow.isSelectionLocked}
						onSelectionChange={workflow.handleSelectionChange}
						onDuplicateDecisionChange={workflow.handleDuplicateDecisionChange}
					/>
					<ImportDialogFooter
						canConfirm={workflow.canConfirm}
						isBusy={workflow.isCloseLocked}
						isCompleted={workflow.isCompleted}
						onCancel={() => workflow.handleOpenChange(false)}
						onConfirm={workflow.handleConfirm}
					/>
				</DialogPrimitive.Content>
			</DialogPrimitive.Portal>
		</DialogPrimitive.Root>
	);
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
	onDesktopSelection,
}: {
	isBusy: boolean;
	onFiles: (files: FileList) => Promise<void>;
	onDesktopSelection: (isDirectory: boolean) => Promise<void>;
}) {
	if (isDesktopClient()) {
		return (
			<div className="grid gap-4 sm:grid-cols-2">
				<Button
					type="button"
					variant="outline"
					disabled={isBusy}
					onClick={() => void onDesktopSelection(false)}
				>
					Select audio files
				</Button>
				<Button
					type="button"
					variant="outline"
					disabled={isBusy}
					onClick={() => void onDesktopSelection(true)}
				>
					Select audio folder
				</Button>
			</div>
		);
	}
	return (
		<div className="grid gap-4 sm:grid-cols-2">
			<ImportFileInput
				id="managed-import-files"
				label="Audio files"
				isBusy={isBusy}
				onFiles={onFiles}
			/>
			<ImportFileInput
				id="managed-import-folder"
				label="Audio folder"
				isBusy={isBusy}
				isDirectory
				onFiles={onFiles}
			/>
		</div>
	);
}

function ImportFileInput({
	id,
	label,
	isBusy,
	isDirectory = false,
	onFiles,
}: {
	id: string;
	label: string;
	isBusy: boolean;
	isDirectory?: boolean;
	onFiles: (files: FileList) => Promise<void>;
}) {
	return (
		<div className="grid gap-2">
			<label htmlFor={id} className="font-medium text-sm">
				{label}
			</label>
			<input
				ref={(input) => {
					if (isDirectory) input?.setAttribute("webkitdirectory", "");
				}}
				id={id}
				type="file"
				multiple
				accept={SUPPORTED_AUDIO_FILE_ACCEPT}
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
	isBusy,
	onSelectionChange,
	onDuplicateDecisionChange,
}: {
	entries: ImportFileEntry[];
	isBusy: boolean;
	onSelectionChange: (key: string, selected: boolean) => void;
	onDuplicateDecisionChange: (key: string, decision: DuplicateDecision) => void;
}) {
	if (entries.length === 0) return null;
	return (
		<section aria-label="Import Preview" className="grid gap-3">
			<h3 className="font-semibold text-heading">Import Preview</h3>
			{entries.map((entry) => (
				<ImportFileRow
					key={entry.key}
					entry={entry}
					isBusy={isBusy}
					onSelectionChange={onSelectionChange}
					onDuplicateDecisionChange={onDuplicateDecisionChange}
				/>
			))}
		</section>
	);
}

function ImportFileRow({
	entry,
	isBusy,
	onSelectionChange,
	onDuplicateDecisionChange,
}: {
	entry: ImportFileEntry;
	isBusy: boolean;
	onSelectionChange: (key: string, selected: boolean) => void;
	onDuplicateDecisionChange: (key: string, decision: DuplicateDecision) => void;
}) {
	const filename = entry.preview?.file.originalFilename ?? entry.file.name;
	const duplicateClassification =
		entry.preview?.duplicateClassification ?? "none";
	return (
		<article className="grid gap-2 rounded-lg border border-border bg-muted/30 p-4">
			<div className="flex items-start justify-between gap-3">
				<div className="flex min-w-0 items-start gap-3">
					{duplicateClassification === "none" ? (
						<input
							type="checkbox"
							aria-label={`Select ${filename}`}
							checked={entry.selected}
							disabled={isBusy || entry.state !== "accepted"}
							onChange={(event) =>
								onSelectionChange(entry.key, event.target.checked)
							}
						/>
					) : null}
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
				</div>
				<span className="rounded-full bg-secondary px-2 py-1 font-medium text-xs">
					{entry.outcome
						? outcomeLabel(entry.outcome)
						: stateLabel(entry.state)}
				</span>
			</div>
			<DuplicateReview
				entry={entry}
				isBusy={isBusy}
				onDecisionChange={onDuplicateDecisionChange}
			/>
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

function DuplicateReview({
	entry,
	isBusy,
	onDecisionChange,
}: {
	entry: ImportFileEntry;
	isBusy: boolean;
	onDecisionChange: (key: string, decision: DuplicateDecision) => void;
}) {
	const preview = entry.preview;
	if (!preview?.duplicateCandidates?.length) return null;
	if (preview.duplicateClassification === "exact_duplicate") {
		return <ExactDuplicateReview preview={preview} />;
	}
	if (preview.duplicateClassification !== "possible_duplicate") return null;
	return (
		<fieldset className="grid gap-2 rounded-md border border-border bg-background p-3">
			<legend className="px-1 font-medium text-heading text-sm">
				Possible Duplicate
			</legend>
			<p className="text-caption text-sm">Different file bytes resemble:</p>
			<ul className="list-disc pl-5 text-caption text-sm">
				{preview.duplicateCandidates.map((candidate) => (
					<li key={candidate.trackId}>
						{candidate.title} — {candidate.artists.join(", ")}
					</li>
				))}
			</ul>
			{duplicateDecisionOptions.map((option) => (
				<DuplicateDecisionOption
					key={option.value}
					entry={entry}
					isBusy={isBusy}
					option={option}
					onDecisionChange={onDecisionChange}
				/>
			))}
		</fieldset>
	);
}

function ExactDuplicateReview({
	preview,
}: {
	preview: NonNullable<ImportFileEntry["preview"]>;
}) {
	const candidate = preview.duplicateCandidates?.[0];
	if (!candidate) return null;
	return (
		<div className="rounded-md border border-border bg-background p-3 text-sm">
			<p className="font-medium text-heading">Exact Duplicate</p>
			<p className="mt-1 text-caption">
				File bytes already belong to {candidate.title}.
			</p>
			<details className="mt-2">
				<summary className="cursor-pointer text-primary underline">
					View existing Track
				</summary>
				<dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-caption">
					<dt>Artist</dt>
					<dd>{candidate.artists.join(", ")}</dd>
					<dt>Album</dt>
					<dd>{candidate.album}</dd>
					<dt>Position</dt>
					<dd>{`${candidate.discNo}.${candidate.trackNo}`}</dd>
				</dl>
			</details>
		</div>
	);
}

function DuplicateDecisionOption({
	entry,
	isBusy,
	option,
	onDecisionChange,
}: {
	entry: ImportFileEntry;
	isBusy: boolean;
	option: (typeof duplicateDecisionOptions)[number];
	onDecisionChange: (key: string, decision: DuplicateDecision) => void;
}) {
	const isReplacement = option.value === "replace_existing";
	return (
		<label className="flex items-center gap-2 text-sm">
			<input
				type="radio"
				name={`duplicate-decision-${entry.key}`}
				value={option.value}
				checked={entry.duplicateDecision === option.value}
				disabled={isBusy || isReplacement}
				title={
					isReplacement
						? "Track Replacement requires the replacement workflow"
						: undefined
				}
				onChange={() => onDecisionChange(entry.key, option.value)}
			/>
			{option.label}
		</label>
	);
}

const duplicateDecisionOptions: Array<{
	value: DuplicateDecision;
	label: string;
}> = [
	{ value: "import_separately", label: "Import separately" },
	{ value: "replace_existing", label: "Replace existing Track" },
	{ value: "do_not_import", label: "Do not import" },
];

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

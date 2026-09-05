import {
	Check,
	CircleAlert,
	CircleCheck,
	CircleX,
	LoaderCircle,
	X,
} from "lucide-react";
import { Dialog as DialogPrimitive } from "radix-ui";
import { memo } from "react";
import { Badge } from "#/components/ui/badge";
import { Button } from "#/components/ui/button";
import { isDesktopClient } from "#/desktop/bridge";
import { cn } from "#/lib/utils.ts";
import { ImportErrors } from "./-import-errors";
import {
	type DuplicateDecision,
	type ImportFileEntry,
	type ImportState,
	SUPPORTED_AUDIO_FILE_ACCEPT,
	summarizeImportEntries,
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
				{/*
				 * The desktop client skips backdrop-blur: the dialog repaints on
				 * every progress tick, and a blurred full-screen backdrop makes
				 * WebKitGTK re-rasterize the whole window for each repaint.
				 */}
				<DialogPrimitive.Overlay
					className={cn(
						"fixed inset-0 z-50 bg-background/80",
						!isDesktopClient() && "backdrop-blur-sm",
					)}
				/>
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
					<ImportSummary
						entries={workflow.entries}
						importState={workflow.importState}
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

function ImportSummary({
	entries,
	importState,
}: {
	entries: ImportFileEntry[];
	importState: ImportState;
}) {
	if (entries.length === 0) return null;
	const summary = summarizeImportEntries(entries);
	const isUploading = importState === "uploading" && summary.unresolved > 0;
	return (
		<section
			aria-label="Import progress"
			className="grid gap-2 rounded-lg border border-border bg-muted/30 p-4"
		>
			<div className="flex items-baseline justify-between gap-3">
				<span className="font-medium text-heading text-sm">
					{isUploading
						? `Processing ${summary.processed} of ${summary.total} files`
						: `${summary.total} ${summary.total === 1 ? "file" : "files"} reviewed`}
				</span>
				<span className="font-semibold text-heading text-lg tabular-nums">
					{summary.percent}%
				</span>
			</div>
			<ProgressBar label="Overall import progress" percent={summary.percent} />
			<div className="flex flex-wrap gap-x-4 gap-y-1 text-caption text-xs">
				{summary.unresolved > 0 ? (
					<SummaryCount count={summary.unresolved} label="in progress" />
				) : null}
				{summary.accepted > 0 ? (
					<SummaryCount count={summary.accepted} label="ready" />
				) : null}
				{summary.needsReview > 0 ? (
					<SummaryCount count={summary.needsReview} label="need review" />
				) : null}
				{summary.rejected > 0 ? (
					<SummaryCount count={summary.rejected} label="rejected" />
				) : null}
				{summary.completed > 0 ? (
					<SummaryCount count={summary.completed} label="imported" />
				) : null}
			</div>
		</section>
	);
}

function ProgressBar({
	label,
	percent,
	isIndeterminate = false,
	className,
}: {
	label: string;
	percent: number;
	isIndeterminate?: boolean;
	className?: string;
}) {
	return (
		<div
			className={cn(
				"h-2 overflow-hidden rounded-full bg-primary/15",
				className,
			)}
			role="progressbar"
			aria-label={label}
			aria-valuenow={percent}
			aria-valuemin={0}
			aria-valuemax={100}
		>
			<div
				className={cn(
					"h-full rounded-full bg-primary",
					isIndeterminate && "opacity-60",
				)}
				style={{ width: `${percent}%` }}
			/>
		</div>
	);
}

function SummaryCount({ count, label }: { count: number; label: string }) {
	return (
		<span>
			<span className="font-medium text-heading tabular-nums">{count}</span>{" "}
			{label}
		</span>
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
		<section aria-label="Import Preview" className="grid gap-2">
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

// Memoized so a progress tick on one upload only re-renders that row.
const ImportFileRow = memo(function ImportFileRow({
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
	const isUnresolved = entry.state === "unresolved";
	const isValidating = isValidatingOnServer(entry);
	return (
		<article className="grid gap-2 rounded-lg border border-border bg-muted/30 px-3 py-2.5">
			<div className="flex items-center gap-3">
				{duplicateClassification === "none" ? (
					<SelectionCheckbox
						label={`Select ${filename}`}
						checked={entry.selected}
						disabled={isBusy || entry.state !== "accepted"}
						onChange={(selected) => onSelectionChange(entry.key, selected)}
					/>
				) : (
					<DuplicateMarker classification={duplicateClassification} />
				)}
				<span className="min-w-0 flex-1">
					<span className="block truncate font-medium text-heading text-sm">
						{entry.preview?.file.title ?? filename}
					</span>
					<RowCaption entry={entry} />
				</span>
				<span className="flex shrink-0 items-center gap-2">
					{isUnresolved ? (
						<span className="text-caption text-xs tabular-nums">
							{entry.progress}%
						</span>
					) : null}
					<StatusBadge entry={entry} />
				</span>
			</div>
			{isUnresolved ? (
				<ProgressBar
					label={`${filename} upload progress`}
					percent={entry.progress}
					isIndeterminate={isValidating}
					className="h-1.5"
				/>
			) : null}
			<DuplicateReview
				entry={entry}
				isBusy={isBusy}
				onDecisionChange={onDuplicateDecisionChange}
			/>
			<ImportErrors message={entry.errorMessage} />
		</article>
	);
});

function isValidatingOnServer(entry: ImportFileEntry): boolean {
	return entry.state === "unresolved" && entry.progress >= 100;
}

function RowCaption({ entry }: { entry: ImportFileEntry }) {
	if (entry.preview) {
		return (
			<span className="block truncate text-caption text-xs">
				<span>{entry.preview.file.artists.join(", ")}</span>
				{" · "}
				<span>{entry.preview.file.album}</span>
			</span>
		);
	}
	if (entry.state !== "unresolved") return null;
	return (
		<span className="block truncate text-caption text-xs">
			{isValidatingOnServer(entry) ? "Validating on the server…" : "Uploading…"}
		</span>
	);
}

function SelectionCheckbox({
	label,
	checked,
	disabled,
	onChange,
}: {
	label: string;
	checked: boolean;
	disabled: boolean;
	onChange: (checked: boolean) => void;
}) {
	return (
		<span className="relative flex size-4 shrink-0 items-center justify-center">
			<input
				type="checkbox"
				aria-label={label}
				checked={checked}
				disabled={disabled}
				onChange={(event) => onChange(event.target.checked)}
				className="peer size-4 cursor-pointer appearance-none rounded border border-input bg-background transition-colors checked:border-primary checked:bg-primary focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-40"
			/>
			<Check
				aria-hidden="true"
				className="pointer-events-none absolute size-3 text-primary-foreground opacity-0 peer-checked:opacity-100"
			/>
		</span>
	);
}

function DuplicateMarker({
	classification,
}: {
	classification:
		| "exact_duplicate"
		| "possible_duplicate"
		| "recording_duplicate";
}) {
	const Icon = classification === "exact_duplicate" ? CircleX : CircleAlert;
	return (
		<Icon
			aria-hidden="true"
			className={cn(
				"size-4 shrink-0",
				classification === "exact_duplicate"
					? "text-destructive"
					: "text-caption",
			)}
		/>
	);
}

function StatusBadge({ entry }: { entry: ImportFileEntry }) {
	const badge = entry.outcome
		? outcomeBadges[entry.outcome]
		: stateBadges[entry.state];
	return (
		<Badge variant={badge.variant}>
			<badge.Icon aria-hidden="true" className={badge.iconClassName} />
			{badge.label}
		</Badge>
	);
}

type StatusBadgeSpec = {
	label: string;
	variant: "default" | "secondary" | "destructive" | "outline";
	iconClassName?: string;
	Icon: typeof Check;
};

const stateBadges: Record<ImportFileEntry["state"], StatusBadgeSpec> = {
	accepted: { label: "Accepted", variant: "outline", Icon: CircleCheck },
	rejected: { label: "Rejected", variant: "destructive", Icon: CircleX },
	unresolved: { label: "Unresolved", variant: "secondary", Icon: LoaderCircle },
	completed: { label: "Completed", variant: "default", Icon: CircleCheck },
};

const outcomeBadges: Record<
	NonNullable<ImportFileEntry["outcome"]>,
	StatusBadgeSpec
> = {
	imported: { label: "Imported", variant: "default", Icon: CircleCheck },
	replaced: { label: "Replaced", variant: "default", Icon: CircleCheck },
	rejected: { label: "Rejected", variant: "destructive", Icon: CircleX },
	failed: { label: "Failed", variant: "destructive", Icon: CircleX },
	not_attempted: {
		label: "Not attempted",
		variant: "secondary",
		Icon: CircleAlert,
	},
};

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
	const isRecordingDuplicate =
		preview.duplicateClassification === "recording_duplicate";
	if (
		preview.duplicateClassification !== "possible_duplicate" &&
		!isRecordingDuplicate
	)
		return null;
	return (
		<fieldset className="grid gap-2 rounded-md border border-border bg-background p-3">
			<legend className="px-1 font-medium text-heading text-sm">
				{isRecordingDuplicate ? "Same recording" : "Possible Duplicate"}
			</legend>
			<p className="text-caption text-sm">
				{isRecordingDuplicate
					? "MusicBrainz identifies this file as the same recording as:"
					: "Different file bytes resemble:"}
			</p>
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

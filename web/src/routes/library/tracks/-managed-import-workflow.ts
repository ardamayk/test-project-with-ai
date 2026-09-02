import {
	ApiError,
	type ManagedImportBatch,
	type ManagedImportBatchFile,
	type ManagedImportDuplicateDecision,
	type ManagedImportPreview,
} from "@repo/api-client";
import { useRef, useState } from "react";
import {
	type DesktopImportSelection,
	desktopUploadImportFile,
	isDesktopClient,
	releaseDesktopImportSelections,
	selectDesktopImportFiles,
	selectDesktopImportFolder,
} from "#/desktop/bridge";
import { apiClient } from "#/lib/api";

export type ImportState = "idle" | "uploading" | "confirming";

const MAX_CONCURRENT_UPLOADS = 3;
const SUPPORTED_AUDIO_EXTENSIONS = [
	"flac",
	"mp3",
	"m4a",
	"ogg",
	"opus",
	"wav",
] as const;
const SUPPORTED_AUDIO_EXTENSION_SET = new Set<string>(
	SUPPORTED_AUDIO_EXTENSIONS,
);
export const SUPPORTED_AUDIO_FILE_ACCEPT = SUPPORTED_AUDIO_EXTENSIONS.map(
	(extension) => `.${extension}`,
).join(",");

export type ImportFileEntry = {
	key: string;
	file: File | DesktopImportSelection;
	jobId?: string;
	progress: number;
	state: "accepted" | "rejected" | "unresolved" | "completed";
	selected: boolean;
	hasSelectionOverride: boolean;
	preview?: ManagedImportPreview;
	errorMessage?: string;
	outcome?: ManagedImportBatchFile["outcome"];
	duplicateDecision?: DuplicateDecision;
};

export type DuplicateDecision = ManagedImportDuplicateDecision["action"];

export function useManagedImportWorkflow({
	onOpenChange,
	onCommitted,
}: {
	onOpenChange: (isOpen: boolean) => void;
	onCommitted: () => Promise<void>;
}) {
	const state = useImportWorkflowState();
	const isBusy = state.importState !== "idle";
	const isCompleted = state.batch?.status === "completed";
	const isCloseLocked =
		(isBusy && !state.batch) ||
		state.importState === "confirming" ||
		state.batch?.status === "confirming";
	const canConfirm = Boolean(
		state.batch &&
			state.entries.length > 0 &&
			!isBusy &&
			!isCompleted &&
			state.batch.files.every(
				(file) =>
					file.state !== "unresolved" ||
					state.entries.some((entry) => entry.jobId === file.jobId),
			) &&
			state.entries.every(
				(entry) =>
					(entry.state !== "unresolved" || Boolean(entry.jobId)) &&
					(entry.preview?.duplicateClassification !== "possible_duplicate" ||
						Boolean(entry.duplicateDecision)),
			),
	);
	return {
		importState: state.importState,
		entries: state.entries,
		errorMessage: state.errorMessage,
		isBusy,
		isCloseLocked,
		isPickerLocked: isBusy || state.entries.length > 0 || Boolean(state.batch),
		isSelectionLocked: isBusy || state.batch?.status === "confirming",
		isCompleted,
		canConfirm,
		handleFiles: createFileHandler(state),
		handleDesktopSelection: createDesktopSelectionHandler(state),
		handleConfirm: createConfirmHandler(state, canConfirm, onCommitted),
		handleSelectionChange: (key: string, selected: boolean) =>
			state.updateEntry(key, { selected, hasSelectionOverride: true }),
		handleDuplicateDecisionChange: (
			key: string,
			duplicateDecision: DuplicateDecision,
		) =>
			state.updateEntry(key, {
				duplicateDecision,
				selected: duplicateDecision !== "do_not_import",
				hasSelectionOverride: true,
			}),
		handleOpenChange: createOpenHandler(state, isCloseLocked, onOpenChange),
	};
}

function useImportWorkflowState() {
	const [importState, setImportState] = useState<ImportState>("idle");
	const [batch, setBatch] = useState<ManagedImportBatch>();
	const [entries, setEntries] = useState<ImportFileEntry[]>([]);
	const [errorMessage, setErrorMessage] = useState("");
	const activeUploadController = useRef<AbortController | undefined>(undefined);
	const isDesktopSelectionPending = useRef(false);
	function updateEntry(key: string, patch: Partial<ImportFileEntry>) {
		setEntries((current) =>
			current.map((entry) =>
				entry.key === key ? { ...entry, ...patch } : entry,
			),
		);
	}
	function reset() {
		setBatch(undefined);
		setEntries([]);
		setErrorMessage("");
	}
	return {
		importState,
		setImportState,
		batch,
		setBatch,
		entries,
		setEntries,
		errorMessage,
		setErrorMessage,
		activeUploadController,
		isDesktopSelectionPending,
		updateEntry,
		reset,
	};
}

type WorkflowState = ReturnType<typeof useImportWorkflowState>;

function createFileHandler(state: WorkflowState) {
	return async (fileList: FileList | Array<File | DesktopImportSelection>) => {
		if (state.entries.length > 0 || state.batch) return;
		const files = Array.from(fileList).filter(isSupportedVisibleAudioFile);
		if (files.length === 0) return;
		state.setImportState("uploading");
		state.setErrorMessage("");
		const initialEntries = files.map(createImportFileEntry);
		state.setEntries(initialEntries);
		const uploadController = new AbortController();
		state.activeUploadController.current = uploadController;
		try {
			const createdBatch = await apiClient.createManagedImportBatch();
			state.setBatch(createdBatch);
			const { batch: previewBatch, entries: uploadedEntries } =
				await uploadImportBatch(
					createdBatch.id,
					initialEntries,
					state.updateEntry,
					uploadController.signal,
				);
			state.setBatch(previewBatch);
			state.setEntries((current) =>
				mergeBatchFiles(
					copyJobAssignments(current, uploadedEntries),
					previewBatch.files,
				),
			);
		} catch (error) {
			if (uploadController.signal.aborted) return;
			state.setErrorMessage(importErrorMessage(error));
		} finally {
			if (state.activeUploadController.current === uploadController) {
				state.activeUploadController.current = undefined;
			}
			state.setImportState("idle");
		}
	};
}

function createDesktopSelectionHandler(state: WorkflowState) {
	const handleFiles = createFileHandler(state);
	return async (isDirectory: boolean) => {
		if (
			state.isDesktopSelectionPending.current ||
			state.entries.length > 0 ||
			state.batch
		) {
			return;
		}
		state.isDesktopSelectionPending.current = true;
		state.setImportState("uploading");
		try {
			const files = await (isDirectory
				? selectDesktopImportFolder()
				: selectDesktopImportFiles());
			state.isDesktopSelectionPending.current = false;
			await handleFiles(files);
		} catch (error) {
			state.setErrorMessage(importErrorMessage(error));
		} finally {
			state.isDesktopSelectionPending.current = false;
			state.setImportState("idle");
		}
	};
}

function isSupportedVisibleAudioFile(
	file: File | DesktopImportSelection,
): boolean {
	if (isDesktopImportSelection(file)) return true;
	const clientPath = file.webkitRelativePath || file.name;
	if (clientPath.split("/").some((segment) => segment.startsWith("."))) {
		return false;
	}
	const extensionSeparator = file.name.lastIndexOf(".");
	if (extensionSeparator < 0) return false;
	return SUPPORTED_AUDIO_EXTENSION_SET.has(
		file.name.slice(extensionSeparator + 1).toLowerCase(),
	);
}

function createConfirmHandler(
	state: WorkflowState,
	canConfirm: boolean,
	onCommitted: () => Promise<void>,
) {
	return async () => {
		if (!state.batch || !canConfirm) return;
		state.setImportState("confirming");
		state.setErrorMessage("");
		try {
			let currentBatch = await apiClient.getManagedImportBatch(state.batch.id);
			let reconciledEntries = attachServerJobs(
				state.entries,
				currentBatch.files,
			);
			if (hasRetryableUploads(reconciledEntries, currentBatch.files)) {
				state.setEntries(reconciledEntries);
				await retryUnresolvedUploads(
					reconciledEntries,
					currentBatch.files,
					state.updateEntry,
				);
				currentBatch = await apiClient.getManagedImportBatch(state.batch.id);
			}
			state.setBatch(currentBatch);
			reconciledEntries = mergeBatchFiles(
				reconciledEntries,
				currentBatch.files,
			);
			state.setEntries(reconciledEntries);
			if (currentBatch.files.some((file) => file.state === "unresolved")) {
				throw new Error("Some files are still unresolved. Retry confirmation.");
			}
			if (hasUndecidedPossibleDuplicate(reconciledEntries)) {
				state.setErrorMessage("Review the newly detected Possible Duplicate.");
				return;
			}
			const report = await confirmImportBatch(currentBatch, reconciledEntries);
			state.setBatch(report);
			state.setEntries((current) => mergeBatchFiles(current, report.files));
			await releaseNativeSelections(reconciledEntries);
			if (hasLibraryMutation(report)) await onCommitted();
		} catch (error) {
			if (
				!(error instanceof ApiError) ||
				error.body.code !== "import_revision_conflict"
			) {
				state.setErrorMessage(importErrorMessage(error));
				return;
			}
			try {
				const isReconciled = await reconcileAfterConfirmationError(state);
				state.setErrorMessage(
					isReconciled
						? "Review the newly detected Possible Duplicate."
						: importErrorMessage(error),
				);
			} catch (refreshError) {
				state.setErrorMessage(
					`${importErrorMessage(error)} Refresh failed: ${importErrorMessage(refreshError)}`,
				);
			}
		} finally {
			state.setImportState("idle");
		}
	};
}

function hasUndecidedPossibleDuplicate(entries: ImportFileEntry[]): boolean {
	return entries.some(
		(entry) =>
			entry.preview?.duplicateClassification === "possible_duplicate" &&
			!entry.duplicateDecision,
	);
}

async function reconcileAfterConfirmationError(
	state: WorkflowState,
): Promise<boolean> {
	if (!state.batch) return false;
	const batch = await apiClient.getManagedImportBatch(state.batch.id);
	const entries = mergeBatchFiles(state.entries, batch.files);
	state.setBatch(batch);
	state.setEntries(entries);
	return hasUndecidedPossibleDuplicate(entries);
}

function createOpenHandler(
	state: WorkflowState,
	isBusy: boolean,
	onOpenChange: (isOpen: boolean) => void,
) {
	return async (nextIsOpen: boolean) => {
		if (isBusy || state.isDesktopSelectionPending.current) return;
		if (nextIsOpen) {
			onOpenChange(true);
			return;
		}
		if (state.batch && state.batch.status !== "completed") {
			const isConfirmed = window.confirm(
				"Cancel this import and remove all uncommitted uploads?",
			);
			if (!isConfirmed) return;
			state.activeUploadController.current?.abort();
			try {
				await apiClient.cancelManagedImportBatch(state.batch.id);
			} catch (error) {
				state.setErrorMessage(importErrorMessage(error));
				return;
			}
		}
		try {
			await releaseNativeSelections(state.entries);
		} catch (error) {
			state.setErrorMessage(importErrorMessage(error));
			return;
		}
		state.reset();
		onOpenChange(false);
	};
}

async function uploadImportBatch(
	batchId: string,
	entries: ImportFileEntry[],
	updateEntry: (key: string, patch: Partial<ImportFileEntry>) => void,
	signal: AbortSignal,
): Promise<{ batch: ManagedImportBatch; entries: ImportFileEntry[] }> {
	const preparedEntries = await createBatchJobs(
		batchId,
		entries,
		updateEntry,
		signal,
	);
	await runWithConcurrency(preparedEntries, ({ entry, jobId }) =>
		uploadFile(jobId, entry, updateEntry, signal),
	);
	signal.throwIfAborted();
	let batch = await apiClient.getManagedImportBatch(batchId);
	let reconciledEntries = attachCreatedJobs(entries, preparedEntries);
	reconciledEntries = attachServerJobs(reconciledEntries, batch.files);
	if (hasRetryableUploads(reconciledEntries, batch.files)) {
		await retryUnresolvedUploads(
			reconciledEntries,
			batch.files,
			updateEntry,
			signal,
		);
		batch = await apiClient.getManagedImportBatch(batchId);
	}
	return { batch, entries: reconciledEntries };
}

async function createBatchJobs(
	batchId: string,
	entries: ImportFileEntry[],
	updateEntry: (key: string, patch: Partial<ImportFileEntry>) => void,
	signal: AbortSignal,
) {
	const preparedEntries: Array<{ entry: ImportFileEntry; jobId: string }> = [];
	for (const entry of entries) {
		signal.throwIfAborted();
		try {
			const job = await apiClient.createManagedImportJob(batchId, entry.key);
			updateEntry(entry.key, { jobId: job.id });
			preparedEntries.push({ entry, jobId: job.id });
		} catch (error) {
			updateEntry(entry.key, {
				state: "rejected",
				errorMessage: importErrorMessage(error),
			});
		}
	}
	return preparedEntries;
}

async function uploadFile(
	jobId: string,
	entry: ImportFileEntry,
	updateEntry: (key: string, patch: Partial<ImportFileEntry>) => void,
	signal?: AbortSignal,
) {
	try {
		const onProgress = (progress: number) =>
			updateEntry(entry.key, { progress });
		const preview = isDesktopImportSelection(entry.file)
			? await uploadDesktopFile(entry.file, jobId, onProgress, signal)
			: await apiClient.uploadManagedImportFile(
					jobId,
					entry.file.name,
					entry.file,
					onProgress,
					signal,
				);
		const duplicateClassification = preview.duplicateClassification ?? "none";
		updateEntry(entry.key, {
			state:
				duplicateClassification === "exact_duplicate" ? "rejected" : "accepted",
			selected: duplicateClassification === "none",
			preview,
			progress: 100,
		});
	} catch (error) {
		if (signal?.aborted) throw error;
		updateEntry(entry.key, {
			state: "rejected",
			selected: false,
			errorMessage: importErrorMessage(error),
		});
	}
}

async function uploadDesktopFile(
	file: DesktopImportSelection,
	jobId: string,
	onProgress: (progress: number) => void,
	signal?: AbortSignal,
): Promise<ManagedImportPreview> {
	const response = await desktopUploadImportFile(
		file.id,
		jobId,
		onProgress,
		signal,
	);
	const body = await response.json();
	if (!response.ok) throw new ApiError(response.status, body);
	return body as ManagedImportPreview;
}

function confirmImportBatch(
	batch: ManagedImportBatch,
	entries: ImportFileEntry[],
) {
	const selectedFileIds = entries.flatMap((entry) =>
		entry.selected && entry.jobId ? [entry.jobId] : [],
	);
	const duplicateDecisions = entries.flatMap((entry) =>
		entry.jobId && entry.duplicateDecision
			? [
					{
						jobId: entry.jobId,
						action: entry.duplicateDecision,
					} satisfies ManagedImportDuplicateDecision,
				]
			: [],
	);
	return duplicateDecisions.length > 0
		? apiClient.confirmManagedImportBatch(
				batch.id,
				batch.revision,
				selectedFileIds,
				duplicateDecisions,
			)
		: apiClient.confirmManagedImportBatch(
				batch.id,
				batch.revision,
				selectedFileIds,
			);
}

function hasLibraryMutation(batch: ManagedImportBatch): boolean {
	return batch.files.some(
		(file) => file.outcome === "imported" || file.outcome === "replaced",
	);
}

async function runWithConcurrency<T>(
	items: T[],
	runItem: (item: T) => Promise<void>,
) {
	let nextIndex = 0;
	async function runWorker() {
		while (nextIndex < items.length) {
			const item = items[nextIndex];
			nextIndex += 1;
			if (item) await runItem(item);
		}
	}
	const concurrencyLimit = isDesktopClient() ? 1 : MAX_CONCURRENT_UPLOADS;
	const workerCount = Math.min(concurrencyLimit, items.length);
	await Promise.all(Array.from({ length: workerCount }, runWorker));
}

function createImportFileEntry(
	file: File | DesktopImportSelection,
): ImportFileEntry {
	return {
		key: crypto.randomUUID(),
		file,
		progress: 0,
		state: "unresolved",
		selected: false,
		hasSelectionOverride: false,
	};
}

function isDesktopImportSelection(
	file: File | DesktopImportSelection,
): file is DesktopImportSelection {
	return !(file instanceof File);
}

function releaseNativeSelections(entries: ImportFileEntry[]): Promise<void> {
	const selectionIds = entries.flatMap((entry) =>
		isDesktopImportSelection(entry.file) ? [entry.file.id] : [],
	);
	return selectionIds.length > 0
		? releaseDesktopImportSelections(selectionIds)
		: Promise.resolve();
}

function mergeBatchFiles(
	entries: ImportFileEntry[],
	files: ManagedImportBatchFile[],
): ImportFileEntry[] {
	return entries.map((entry) => {
		const result = files.find((file) => file.jobId === entry.jobId);
		if (!result) return entry;
		const serverError = result.errorReason ?? result.errorCode;
		return {
			...entry,
			state: result.state,
			selected:
				result.state === "accepted" && entry.hasSelectionOverride
					? entry.selected
					: result.selected,
			preview: result.preview ?? entry.preview,
			progress: result.validationProgress,
			errorMessage:
				result.state === "accepted" || result.state === "completed"
					? serverError
					: (serverError ?? entry.errorMessage),
			outcome: result.outcome,
		};
	});
}

function attachCreatedJobs(
	entries: ImportFileEntry[],
	preparedEntries: Array<{ entry: ImportFileEntry; jobId: string }>,
): ImportFileEntry[] {
	return entries.map((entry) => {
		const prepared = preparedEntries.find(
			(item) => item.entry.key === entry.key,
		);
		return prepared ? { ...entry, jobId: prepared.jobId } : entry;
	});
}

function copyJobAssignments(
	entries: ImportFileEntry[],
	assignedEntries: ImportFileEntry[],
): ImportFileEntry[] {
	return entries.map((entry) => {
		const assigned = assignedEntries.find((item) => item.key === entry.key);
		return assigned?.jobId ? { ...entry, jobId: assigned.jobId } : entry;
	});
}

function attachServerJobs(
	entries: ImportFileEntry[],
	files: ManagedImportBatchFile[],
): ImportFileEntry[] {
	return entries.map((entry) => {
		if (entry.jobId) return entry;
		const file = files.find(
			(candidate) =>
				candidate.state === "unresolved" &&
				candidate.clientFileId === entry.key,
		);
		return file
			? {
					...entry,
					jobId: file.jobId,
					state: "unresolved",
					errorMessage: undefined,
				}
			: entry;
	});
}

function hasRetryableUploads(
	entries: ImportFileEntry[],
	files: ManagedImportBatchFile[],
): boolean {
	return entries.some(
		(entry) =>
			entry.jobId &&
			files.some(
				(file) => file.jobId === entry.jobId && file.state === "unresolved",
			),
	);
}

async function retryUnresolvedUploads(
	entries: ImportFileEntry[],
	files: ManagedImportBatchFile[],
	updateEntry: (key: string, patch: Partial<ImportFileEntry>) => void,
	signal?: AbortSignal,
) {
	const retryableEntries = entries.flatMap((entry) => {
		const isUnresolved = files.some(
			(file) => file.jobId === entry.jobId && file.state === "unresolved",
		);
		return entry.jobId && isUnresolved ? [{ entry, jobId: entry.jobId }] : [];
	});
	await runWithConcurrency(retryableEntries, async ({ entry, jobId }) => {
		updateEntry(entry.key, {
			state: "unresolved",
			progress: 0,
			errorMessage: undefined,
		});
		await uploadFile(jobId, entry, updateEntry, signal);
	});
}

function importErrorMessage(error: unknown): string {
	if (error instanceof Error && error.message.trim()) return error.message;
	if (
		typeof error === "object" &&
		error !== null &&
		"message" in error &&
		typeof error.message === "string" &&
		error.message.trim()
	) {
		return error.message;
	}
	return "Managed Import failed. Please try again.";
}

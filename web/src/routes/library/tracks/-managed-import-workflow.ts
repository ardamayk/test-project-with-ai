import type {
	ManagedImportBatch,
	ManagedImportBatchFile,
	ManagedImportPreview,
} from "@repo/api-client";
import { useState } from "react";
import { isDesktopClient } from "#/desktop/bridge";
import { apiClient } from "#/lib/api";

export type ImportState = "idle" | "uploading" | "confirming";

const MAX_CONCURRENT_UPLOADS = 3;

export type ImportFileEntry = {
	key: string;
	file: File;
	jobId?: string;
	progress: number;
	state: "accepted" | "rejected" | "unresolved" | "completed";
	selected: boolean;
	hasSelectionOverride: boolean;
	preview?: ManagedImportPreview;
	errorMessage?: string;
	outcome?: ManagedImportBatchFile["outcome"];
};

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
				(entry) => entry.state !== "unresolved" || Boolean(entry.jobId),
			),
	);
	return {
		importState: state.importState,
		entries: state.entries,
		errorMessage: state.errorMessage,
		isBusy,
		isCompleted,
		canConfirm,
		handleFiles: createFileHandler(state),
		handleConfirm: createConfirmHandler(state, canConfirm, onCommitted),
		handleSelectionChange: (key: string, selected: boolean) =>
			state.updateEntry(key, { selected, hasSelectionOverride: true }),
		handleOpenChange: createOpenHandler(state, isBusy, onOpenChange),
	};
}

function useImportWorkflowState() {
	const [importState, setImportState] = useState<ImportState>("idle");
	const [batch, setBatch] = useState<ManagedImportBatch>();
	const [entries, setEntries] = useState<ImportFileEntry[]>([]);
	const [errorMessage, setErrorMessage] = useState("");
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
		updateEntry,
		reset,
	};
}

type WorkflowState = ReturnType<typeof useImportWorkflowState>;

function createFileHandler(state: WorkflowState) {
	return async (fileList: FileList | File[]) => {
		const files = Array.from(fileList);
		if (files.length === 0) return;
		state.setImportState("uploading");
		state.setErrorMessage("");
		const initialEntries = files.map(createImportFileEntry);
		state.setEntries(initialEntries);
		try {
			const createdBatch = await apiClient.createManagedImportBatch();
			state.setBatch(createdBatch);
			const { batch: previewBatch, entries: uploadedEntries } =
				await uploadImportBatch(
					createdBatch.id,
					initialEntries,
					state.updateEntry,
				);
			state.setBatch(previewBatch);
			state.setEntries((current) =>
				mergeBatchFiles(
					copyJobAssignments(current, uploadedEntries),
					previewBatch.files,
				),
			);
		} catch (error) {
			state.setErrorMessage(importErrorMessage(error));
		} finally {
			state.setImportState("idle");
		}
	};
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
			const report = await confirmImportBatch(currentBatch, reconciledEntries);
			state.setBatch(report);
			state.setEntries((current) => mergeBatchFiles(current, report.files));
			if (hasLibraryMutation(report)) await onCommitted();
		} catch (error) {
			state.setErrorMessage(importErrorMessage(error));
		} finally {
			state.setImportState("idle");
		}
	};
}

function createOpenHandler(
	state: WorkflowState,
	isBusy: boolean,
	onOpenChange: (isOpen: boolean) => void,
) {
	return (nextIsOpen: boolean) => {
		if (isBusy) return;
		if (!nextIsOpen) state.reset();
		onOpenChange(nextIsOpen);
	};
}

async function uploadImportBatch(
	batchId: string,
	entries: ImportFileEntry[],
	updateEntry: (key: string, patch: Partial<ImportFileEntry>) => void,
): Promise<{ batch: ManagedImportBatch; entries: ImportFileEntry[] }> {
	const preparedEntries = await createBatchJobs(batchId, entries, updateEntry);
	await runWithConcurrency(preparedEntries, ({ entry, jobId }) =>
		uploadFile(jobId, entry, updateEntry),
	);
	let batch = await apiClient.getManagedImportBatch(batchId);
	let reconciledEntries = attachCreatedJobs(entries, preparedEntries);
	reconciledEntries = attachServerJobs(reconciledEntries, batch.files);
	if (hasRetryableUploads(reconciledEntries, batch.files)) {
		await retryUnresolvedUploads(reconciledEntries, batch.files, updateEntry);
		batch = await apiClient.getManagedImportBatch(batchId);
	}
	return { batch, entries: reconciledEntries };
}

async function createBatchJobs(
	batchId: string,
	entries: ImportFileEntry[],
	updateEntry: (key: string, patch: Partial<ImportFileEntry>) => void,
) {
	const preparedEntries: Array<{ entry: ImportFileEntry; jobId: string }> = [];
	for (const entry of entries) {
		try {
			const job = await apiClient.createManagedImportJob(batchId);
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
) {
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

function confirmImportBatch(
	batch: ManagedImportBatch,
	entries: ImportFileEntry[],
) {
	const selectedFileIds = entries.flatMap((entry) =>
		entry.selected && entry.jobId ? [entry.jobId] : [],
	);
	return apiClient.confirmManagedImportBatch(
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

function createImportFileEntry(file: File, index: number): ImportFileEntry {
	return {
		key: `${index}:${file.name}:${file.size}`,
		file,
		progress: 0,
		state: "unresolved",
		selected: false,
		hasSelectionOverride: false,
	};
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
	const knownJobIds = new Set(entries.flatMap((entry) => entry.jobId ?? []));
	const serverOnlyJobs = files.filter(
		(file) => file.state === "unresolved" && !knownJobIds.has(file.jobId),
	);
	let nextServerJob = 0;
	return entries.map((entry) => {
		if (entry.jobId || nextServerJob >= serverOnlyJobs.length) return entry;
		const file = serverOnlyJobs[nextServerJob];
		nextServerJob += 1;
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
) {
	const retryableEntries = entries.flatMap((entry) => {
		const isUnresolved = files.some(
			(file) => file.jobId === entry.jobId && file.state === "unresolved",
		);
		return entry.jobId && isUnresolved ? [{ entry, jobId: entry.jobId }] : [];
	});
	await runWithConcurrency(retryableEntries, ({ entry, jobId }) =>
		uploadFile(jobId, entry, updateEntry),
	);
}

function importErrorMessage(error: unknown): string {
	if (error instanceof Error && error.message.trim()) return error.message;
	return "Managed Import failed. Please try again.";
}

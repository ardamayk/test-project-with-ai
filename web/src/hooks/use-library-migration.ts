import type {
	LibraryMigrationCleanup,
	LibraryMigrationCleanupPreview,
	LibraryMigrationCutover,
	LibraryMigrationPreview,
	LibraryMigrationStage,
} from "@repo/api-client";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { apiClient } from "#/lib/api";

export type LibraryMigrationStep = "idle" | "previewed" | "staged" | "migrated";

export type LibraryMigrationAction =
	| "preview"
	| "stage"
	| "cutover"
	| "cleanupPreview"
	| "cleanup";

export type LibraryMigrationError = {
	action: LibraryMigrationAction;
	message: string;
};

/**
 * Drives Library Migration as explicit, separately confirmed steps: preview,
 * stage, cutover, then an optional Legacy Source Cleanup. Every step calls the
 * Music Server, which owns validation, idempotency, and the destructive
 * decisions; this hook only sequences the calls and keeps the latest report.
 */
export function useLibraryMigration({
	onCutover,
}: {
	onCutover?: (result: LibraryMigrationCutover) => void;
} = {}) {
	const queryClient = useQueryClient();
	const [step, setStep] = useState<LibraryMigrationStep>("idle");
	const [preview, setPreview] = useState<LibraryMigrationPreview | null>(null);
	const [stage, setStage] = useState<LibraryMigrationStage | null>(null);
	const [cutover, setCutover] = useState<LibraryMigrationCutover | null>(null);
	const [cleanupPreview, setCleanupPreview] =
		useState<LibraryMigrationCleanupPreview | null>(null);
	const [cleanup, setCleanup] = useState<LibraryMigrationCleanup | null>(null);
	const [error, setError] = useState<LibraryMigrationError | null>(null);

	const invalidateLibrary = () =>
		queryClient.invalidateQueries({ queryKey: ["library"] });
	const fail = (action: LibraryMigrationAction) => (cause: Error) =>
		setError({ action, message: cause.message });

	const previewMutation = useMutation({
		mutationFn: () => apiClient.previewLibraryMigration(),
		onMutate: () => {
			setError(null);
			setStage(null);
			setCutover(null);
			setCleanupPreview(null);
			setCleanup(null);
		},
		onSuccess: (result) => {
			setPreview(result);
			setStep("previewed");
		},
		onError: fail("preview"),
	});
	const stageMutation = useMutation({
		mutationFn: () => apiClient.stageLibraryMigration(),
		onMutate: () => setError(null),
		onSuccess: (result) => {
			setStage(result);
			setStep("staged");
		},
		onError: fail("stage"),
	});
	const cutoverMutation = useMutation({
		mutationFn: () => apiClient.cutoverLibraryMigration(),
		onMutate: () => setError(null),
		onSuccess: async (result) => {
			setCutover(result);
			setStep("migrated");
			onCutover?.(result);
			await invalidateLibrary();
		},
		onError: fail("cutover"),
	});
	const cleanupPreviewMutation = useMutation({
		mutationFn: () => apiClient.previewLibraryMigrationCleanup(),
		onMutate: () => {
			setError(null);
			setCleanup(null);
		},
		onSuccess: setCleanupPreview,
		onError: fail("cleanupPreview"),
	});
	const cleanupMutation = useMutation({
		mutationFn: (selection: LibraryMigrationCleanupPreview) =>
			apiClient.cleanupLibraryMigrationSources({
				trackIds: selection.files
					.filter((file) => file.state === "eligible")
					.map((file) => file.trackId),
				fileCount: selection.eligibleCount,
				totalSizeBytes: selection.totalSizeBytes,
			}),
		onMutate: () => setError(null),
		onSuccess: (result) => {
			setCleanup(result);
			setCleanupPreview(null);
		},
		onError: fail("cleanup"),
	});

	const isBusy =
		previewMutation.isPending ||
		stageMutation.isPending ||
		cutoverMutation.isPending ||
		cleanupPreviewMutation.isPending ||
		cleanupMutation.isPending;

	return {
		step,
		preview,
		stage,
		cutover,
		cleanupPreview,
		cleanup,
		error,
		isBusy,
		isPreviewing: previewMutation.isPending,
		isStaging: stageMutation.isPending,
		isCuttingOver: cutoverMutation.isPending,
		isLoadingCleanup: cleanupPreviewMutation.isPending,
		isCleaning: cleanupMutation.isPending,
		runPreview: () => previewMutation.mutate(),
		runStage: () => stageMutation.mutate(),
		runCutover: () => cutoverMutation.mutate(),
		loadCleanupPreview: () => cleanupPreviewMutation.mutate(),
		dismissCleanupPreview: () => {
			if (cleanupMutation.isPending) return;
			setCleanupPreview(null);
			setError((current) => (current?.action === "cleanup" ? null : current));
		},
		runCleanup: () => {
			if (cleanupPreview && cleanupPreview.eligibleCount > 0)
				cleanupMutation.mutate(cleanupPreview);
		},
	};
}

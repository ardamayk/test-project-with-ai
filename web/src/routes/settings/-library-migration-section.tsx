import {
	LIBRARY_MIGRATION_CAPABILITY,
	type LibraryMigrationCleanup,
	type LibraryMigrationCutover,
	type LibraryMigrationPreview,
	type LibraryMigrationStage,
} from "@repo/api-client";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Button } from "#/components/ui/button";
import { useLibraryMigration } from "#/hooks/use-library-migration";
import { useReturnFocus } from "#/hooks/use-return-focus";
import { useServerCapabilityState } from "#/hooks/use-server-capability";
import { apiClient } from "#/lib/api";
import {
	LegacySourceCleanupDialog,
	MigrationCutoverDialog,
} from "./-library-migration-dialogs";
import { migrationFileReason } from "./-library-migration-format";

const MIGRATION_UNSUPPORTED_TITLE =
	"This Music Server does not support Library Migration. Update the Music Server to migrate legacy tracks.";

export function LibraryMigrationSection() {
	const capability = useServerCapabilityState(LIBRARY_MIGRATION_CAPABILITY);
	const legacy = useLegacyTrackCount();
	const [isCutoverOpen, setIsCutoverOpen] = useState(false);
	// The cutover dialog closes once a report arrives; the report itself stays
	// rendered in the section so the outcome remains reviewable.
	const migration = useLibraryMigration({
		onCutover: () => setIsCutoverOpen(false),
	});
	const returnFocus = useReturnFocus();
	const isUnsupported = capability === "missing";

	const sectionError =
		migration.error &&
		migration.error.action !== "cutover" &&
		migration.error.action !== "cleanup"
			? migration.error.message
			: null;
	const dialogError = (action: "cutover" | "cleanup") =>
		migration.error?.action === action ? migration.error.message : null;

	const openCutover = () => {
		returnFocus.capture();
		setIsCutoverOpen(true);
	};
	const openCleanup = () => {
		returnFocus.capture();
		migration.loadCleanupPreview();
	};

	return (
		<section
			className="mb-8 flex flex-col gap-4"
			aria-labelledby="library-migration-heading"
		>
			<div>
				<h2 id="library-migration-heading" className="font-medium text-sm">
					Library Migration
				</h2>
				<p className="text-caption text-sm">
					Move Legacy Tracks from the old music folder into Managed Storage
					using the Strict Import Profile. Nothing changes until each step is
					confirmed, and legacy source files are never deleted by migration.
				</p>
			</div>
			<LegacyTrackCount {...legacy} />
			<p aria-live="polite" className="text-caption text-sm">
				{migrationStatusText(migration)}
			</p>
			{sectionError ? (
				<p role="alert" className="text-destructive text-sm">
					{sectionError}
				</p>
			) : null}
			<div className="flex flex-wrap gap-2">
				<Button
					type="button"
					size="sm"
					onClick={migration.runPreview}
					disabled={isUnsupported || migration.isBusy}
					title={isUnsupported ? MIGRATION_UNSUPPORTED_TITLE : undefined}
				>
					{migration.isPreviewing ? "Analyzing…" : "Analyze library"}
				</Button>
				<Button
					type="button"
					size="sm"
					variant="secondary"
					onClick={migration.runStage}
					disabled={
						migration.isBusy ||
						migration.step === "idle" ||
						migration.step === "migrated" ||
						(migration.preview?.acceptedCount ?? 0) === 0
					}
				>
					{migration.isStaging ? "Copying…" : "Copy and verify"}
				</Button>
				<Button
					type="button"
					size="sm"
					variant="secondary"
					onClick={openCutover}
					disabled={
						migration.isBusy ||
						migration.step !== "staged" ||
						(migration.stage?.verifiedCount ?? 0) === 0
					}
				>
					Activate migrated Tracks…
				</Button>
				<Button
					type="button"
					size="sm"
					variant="outline"
					onClick={openCleanup}
					disabled={isUnsupported || migration.isBusy}
					title={isUnsupported ? MIGRATION_UNSUPPORTED_TITLE : undefined}
				>
					{migration.isLoadingCleanup
						? "Checking sources…"
						: "Clean up legacy sources…"}
				</Button>
			</div>
			{migration.preview ? (
				<MigrationPreviewReport preview={migration.preview} />
			) : null}
			{migration.stage ? (
				<MigrationStageReport stage={migration.stage} />
			) : null}
			{migration.cutover ? (
				<MigrationCutoverReport cutover={migration.cutover} />
			) : null}
			{migration.cleanup ? (
				<MigrationCleanupReport cleanup={migration.cleanup} />
			) : null}
			<MigrationCutoverDialog
				isOpen={isCutoverOpen}
				verifiedCount={migration.stage?.verifiedCount ?? 0}
				error={dialogError("cutover")}
				isCuttingOver={migration.isCuttingOver}
				onCancel={() => {
					if (!migration.isCuttingOver) setIsCutoverOpen(false);
				}}
				onConfirm={() => {
					migration.runCutover();
				}}
				onCloseAutoFocus={returnFocus.restore}
			/>
			<LegacySourceCleanupDialog
				preview={migration.cleanupPreview}
				error={dialogError("cleanup")}
				isCleaning={migration.isCleaning}
				onCancel={migration.dismissCleanupPreview}
				onConfirm={migration.runCleanup}
				onCloseAutoFocus={returnFocus.restore}
			/>
		</section>
	);
}

const LEGACY_TRACK_SAMPLE_LIMIT = 200;

/**
 * The versioned contract has no Legacy Track count endpoint, so the count is
 * derived from the first page of the Track list and marked as a lower bound
 * when the library is larger than that page.
 */
function useLegacyTrackCount() {
	const tracks = useQuery({
		queryKey: ["library", "tracks", "legacy-sample"],
		queryFn: () => apiClient.listTracks({ limit: LEGACY_TRACK_SAMPLE_LIMIT }),
	});
	const items = tracks.data?.items ?? [];
	const legacyCount = items.filter(
		(track) => track.sourceKind === "legacy",
	).length;
	const isPartial = (tracks.data?.total ?? 0) > items.length;
	return {
		isLoading: tracks.isLoading,
		isError: tracks.isError,
		legacyCount,
		isPartial,
	};
}

function LegacyTrackCount({
	isLoading,
	isError,
	legacyCount,
	isPartial,
}: ReturnType<typeof useLegacyTrackCount>) {
	if (isLoading)
		return <p className="text-foreground text-sm">Counting Legacy Tracks…</p>;
	if (isError)
		return (
			<p className="text-destructive text-sm">Failed to load the library</p>
		);
	if (legacyCount === 0 && isPartial)
		return (
			<p className="text-foreground text-sm" data-testid="legacy-track-count">
				No Legacy Tracks among the first {LEGACY_TRACK_SAMPLE_LIMIT} loaded
				Tracks. Analyze the library to check every Track.
			</p>
		);
	const prefix = isPartial ? "At least " : "";
	return (
		<p className="text-foreground text-sm" data-testid="legacy-track-count">
			{prefix}
			{legacyCount} Legacy Track{legacyCount === 1 ? "" : "s"}
			{legacyCount === 0 ? " — nothing to migrate" : ""}
		</p>
	);
}

function migrationStatusText(
	migration: ReturnType<typeof useLibraryMigration>,
): string {
	if (migration.isPreviewing) return "Analyzing every Legacy Track…";
	if (migration.isStaging) return "Copying and verifying accepted files…";
	if (migration.isCuttingOver) return "Activating verified copies…";
	if (migration.isCleaning) return "Deleting confirmed legacy sources…";
	switch (migration.step) {
		case "idle":
			return "Analyze the library to see which Legacy Tracks can be migrated.";
		case "previewed":
			if (migration.preview && migration.preview.files.length === 0)
				return "No Legacy Tracks remain. Every Legacy Track has already been migrated or none exists.";
			if (migration.preview?.acceptedCount === 0)
				return "No file was accepted. Repair the rejected files and analyze again.";
			return "Review the analysis, then copy and verify the accepted files.";
		case "staged":
			return "Verified copies are ready. Activate them to finish the migration.";
		case "migrated":
			if (migration.cutover?.migratedCount === 0)
				return "Nothing was activated. Verified copies were already migrated or none remained.";
			return "Migration finished. Legacy source files are still on disk until you clean them up.";
	}
}

const previewStateLabel: Record<
	LibraryMigrationPreview["files"][number]["state"],
	string
> = { accepted: "Accepted", rejected: "Rejected" };
const stageStateLabel: Record<
	LibraryMigrationStage["files"][number]["state"],
	string
> = { verified: "Verified", rejected: "Rejected", failed: "Failed" };
const cutoverStateLabel: Record<
	LibraryMigrationCutover["files"][number]["state"],
	string
> = {
	migrated: "Migrated",
	rejected: "Rejected",
	failed: "Failed",
	not_attempted: "Not attempted",
};
const cleanupStateLabel: Record<
	LibraryMigrationCleanup["files"][number]["state"],
	string
> = { deleted: "Deleted", failed: "Failed" };

function MigrationFileTable({
	caption,
	rows,
}: {
	caption: string;
	rows: Array<{ key: string; name: string; state: string; detail: string }>;
}) {
	if (rows.length === 0)
		return <output className="block text-caption text-sm">{caption}</output>;
	return (
		<div className="overflow-x-auto rounded-lg border border-border">
			<table className="w-full text-sm">
				<caption className="px-3 py-2 text-left text-caption">
					{caption}
				</caption>
				<thead>
					<tr className="text-left text-caption">
						<th scope="col" className="px-3 py-2 font-medium">
							File
						</th>
						<th scope="col" className="px-3 py-2 font-medium">
							Result
						</th>
						<th scope="col" className="px-3 py-2 font-medium">
							Details
						</th>
					</tr>
				</thead>
				<tbody>
					{rows.map((row) => (
						<tr key={row.key} className="border-border border-t">
							<td className="break-all px-3 py-2 text-foreground">
								{row.name}
							</td>
							<td className="px-3 py-2 text-foreground">{row.state}</td>
							<td className="px-3 py-2 text-caption">{row.detail}</td>
						</tr>
					))}
				</tbody>
			</table>
		</div>
	);
}

function previewResultLabel(
	file: LibraryMigrationPreview["files"][number],
): string {
	if (file.state === "rejected" && file.errorCode === "exact_duplicate")
		return "Exact Duplicate";
	if (file.state === "rejected" && file.errorField === "capacity")
		return "Needs space";
	return previewStateLabel[file.state];
}

function MigrationPreviewReport({
	preview,
}: {
	preview: LibraryMigrationPreview;
}) {
	const capacityRejections = preview.files.filter(
		(file) => file.state === "rejected" && file.errorField === "capacity",
	).length;
	return (
		<>
			{capacityRejections > 0 ? (
				<p role="alert" className="text-destructive text-sm">
					Managed Storage does not have enough free space for{" "}
					{capacityRejections} otherwise accepted file
					{capacityRejections === 1 ? "" : "s"} plus the safety reserve. Free
					space and analyze again.
				</p>
			) : null}
			<MigrationFileTable
				caption={`Analysis: ${preview.acceptedCount} accepted, ${preview.rejectedCount} rejected. Rejected files stay untouched at their source.`}
				rows={preview.files.map((file) => ({
					key: file.trackId,
					name: file.originalFilename,
					state: previewResultLabel(file),
					detail:
						file.state === "accepted"
							? [file.preview?.albumArtists.join(", "), file.preview?.album]
									.filter(Boolean)
									.join(" · ")
							: migrationFileReason(file),
				}))}
			/>
		</>
	);
}

function MigrationStageReport({ stage }: { stage: LibraryMigrationStage }) {
	return (
		<MigrationFileTable
			caption={`Copies: ${stage.verifiedCount} verified, ${stage.rejectedCount} rejected, ${stage.failedCount} failed.`}
			rows={stage.files.map((file) => ({
				key: file.trackId,
				name: file.originalFilename,
				state: stageStateLabel[file.state],
				detail:
					file.state === "verified"
						? `SHA-256 ${file.pendingSha256?.slice(0, 12) ?? ""}… verified`
						: migrationFileReason(file),
			}))}
		/>
	);
}

function MigrationCutoverReport({
	cutover,
}: {
	cutover: LibraryMigrationCutover;
}) {
	return (
		<MigrationFileTable
			caption={`Cutover: ${cutover.migratedCount} migrated, ${cutover.rejectedCount} rejected, ${cutover.failedCount} failed, ${cutover.notAttemptedCount} not attempted.`}
			rows={cutover.files.map((file) => ({
				key: file.trackId,
				name: file.originalFilename,
				state: cutoverStateLabel[file.state],
				detail:
					file.state === "migrated"
						? `New Track ID ${file.createdTrackId ?? ""}`
						: migrationFileReason(file),
			}))}
		/>
	);
}

function MigrationCleanupReport({
	cleanup,
}: {
	cleanup: LibraryMigrationCleanup;
}) {
	return (
		<MigrationFileTable
			caption={`Cleanup: ${cleanup.deletedCount} deleted, ${cleanup.failedCount} failed, ${cleanup.prunedDirectoryCount} empty folder${cleanup.prunedDirectoryCount === 1 ? "" : "s"} pruned.`}
			rows={cleanup.files.map((file) => ({
				key: file.trackId,
				name: file.originalFilename,
				state: cleanupStateLabel[file.state],
				detail:
					file.state === "deleted"
						? "Legacy source removed"
						: migrationFileReason(file),
			}))}
		/>
	);
}

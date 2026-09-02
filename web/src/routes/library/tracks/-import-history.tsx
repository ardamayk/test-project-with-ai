import type { ManagedImportHistoryItem } from "@repo/api-client";
import { RotateCcw } from "lucide-react";
import { Button } from "#/components/ui/button";

export function ImportHistory({
	items,
	isLoading,
	isError,
	onRetry,
}: {
	items: ManagedImportHistoryItem[];
	isLoading: boolean;
	isError: boolean;
	onRetry: () => void;
}) {
	return (
		<section
			className="space-y-3 border-border border-t pt-5"
			aria-labelledby="import-history-heading"
		>
			<div className="flex items-center justify-between gap-3">
				<div>
					<h2
						id="import-history-heading"
						className="font-semibold text-heading text-lg"
					>
						Import History
					</h2>
					<p className="text-caption text-xs">
						Latest terminal Managed Import results
					</p>
				</div>
				<Button
					type="button"
					variant="outline"
					size="sm"
					aria-label="Retry import"
					onClick={onRetry}
				>
					<RotateCcw className="size-4" />
					Retry
				</Button>
			</div>
			{isLoading ? (
				<p className="text-foreground text-sm">Loading Import History…</p>
			) : null}
			{isError ? (
				<p className="text-destructive text-sm">
					Failed to load Import History
				</p>
			) : null}
			{!isLoading && !isError && items.length === 0 ? (
				<p className="text-foreground text-sm">No terminal imports yet.</p>
			) : null}
			<div className="space-y-2">
				{items.map((item) => (
					<ImportHistoryCard key={item.importId} item={item} />
				))}
			</div>
		</section>
	);
}

function ImportHistoryCard({ item }: { item: ManagedImportHistoryItem }) {
	return (
		<details className="rounded-xl border border-border bg-card px-4 py-3">
			<summary className="cursor-pointer list-none">
				<div className="flex flex-wrap items-center justify-between gap-2">
					<div>
						<p className="font-medium text-heading text-sm">
							{resultLabel(item.resultCode)}
						</p>
						<p className="text-caption text-xs">{countSummary(item)}</p>
					</div>
					<time className="text-caption text-xs" dateTime={item.completedAt}>
						{new Date(item.completedAt).toLocaleString()}
					</time>
				</div>
			</summary>
			<div className="mt-3 space-y-2 border-border border-t pt-3">
				<p className="break-all font-mono text-caption text-xs">
					Import {item.importId}
				</p>
				<p className="text-caption text-xs">
					{item.startedAt} → {item.completedAt}
				</p>
				{item.files.map((file) => (
					<div
						key={file.fileId}
						className="rounded-lg bg-secondary/50 p-3 text-xs"
					>
						<p className="font-medium text-heading">
							{file.safeFilename ?? "Filename unavailable"}
						</p>
						<p className="break-all text-foreground">
							Result: {file.resultCode}
						</p>
						<p className="break-all text-caption">
							File {file.fileId} · Job {file.jobId}
						</p>
						{file.contentSha256 ? (
							<p className="break-all font-mono text-caption">
								SHA-256 {file.contentSha256}
							</p>
						) : null}
						{file.createdTrackId ? (
							<p className="break-all text-caption">
								Created Track {file.createdTrackId}
							</p>
						) : null}
						{file.replacedTrackId ? (
							<p className="break-all text-caption">
								Replaced Track {file.replacedTrackId}
							</p>
						) : null}
					</div>
				))}
			</div>
		</details>
	);
}

function resultLabel(
	resultCode: ManagedImportHistoryItem["resultCode"],
): string {
	return {
		completed: "Completed",
		partially_completed: "Partially completed",
		failed: "Failed",
		canceled: "Canceled",
	}[resultCode];
}

function countSummary(item: ManagedImportHistoryItem): string {
	const labels = [
		[item.counts.imported, "imported"],
		[item.counts.replaced, "replaced"],
		[item.counts.rejected, "rejected"],
		[item.counts.failed, "failed"],
		[item.counts.notAttempted, "not attempted"],
		[item.counts.canceled, "canceled"],
	] as const;
	return (
		labels
			.filter(([count]) => count > 0)
			.map(([count, label]) => `${count} ${label}`)
			.join(" · ") || "No files"
	);
}

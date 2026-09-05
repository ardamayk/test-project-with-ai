import { useEffect, useState } from "react";
import type { ImportFileEntry } from "./-managed-import-workflow";

const BYTES_PER_MIB = 1024 * 1024;
const PROGRESS_CLOCK_INTERVAL_MS = 1000;
const PHASE_LABELS = {
	queued: "Queued",
	uploading: "Uploading…",
	validating: "Validating…",
	waiting_identification: "Waiting for identification…",
	identifying: "Identifying…",
	ready: "Ready",
	failed: "Failed",
	committing: "Importing…",
	completed: "Completed",
	retry_wait: "Waiting to retry…",
};

export function importPhaseLabel(entry: ImportFileEntry): string {
	return PHASE_LABELS[entry.phase ?? "queued"];
}

function formatBytes(bytes: number) {
	return `${(bytes / BYTES_PER_MIB).toFixed(1)} MiB`;
}

export function ImportTransferDetails({ entry }: { entry: ImportFileEntry }) {
	const [now, setNow] = useState(Date.now);
	const [isExpanded, setIsExpanded] = useState(false);
	const isActive =
		entry.phase === "retry_wait" ||
		(isExpanded && entry.state === "unresolved" && entry.phase !== "queued");
	useEffect(() => {
		if (!isActive) return;
		setNow(Date.now());
		const timer = setInterval(
			() => setNow(Date.now()),
			PROGRESS_CLOCK_INTERVAL_MS,
		);
		return () => clearInterval(timer);
	}, [isActive]);
	const elapsed = Math.max(
		0,
		Math.floor((now - (entry.startedAt ?? now)) / 1000),
	);
	const bytes = entry.transferredBytes ?? 0;
	return (
		<div className="text-caption text-xs">
			{entry.phase === "retry_wait" ? (
				<p>
					Retrying in{" "}
					{Math.max(0, Math.ceil(((entry.retryAt ?? now) - now) / 1000))}s ·{" "}
					{entry.retryCount}/3
				</p>
			) : null}
			{entry.phase === "uploading" ? (
				<p>
					{formatBytes(bytes)} / {formatBytes(entry.file.size)}
				</p>
			) : null}
			{entry.startedAt || entry.errorMessage ? (
				<details onToggle={(event) => setIsExpanded(event.currentTarget.open)}>
					<summary className="cursor-pointer">Transfer details</summary>
					<p>
						{elapsed}s elapsed · {entry.retryCount ?? 0} retries
					</p>
					{entry.phase === "uploading" && elapsed > 0 ? (
						<p>{formatBytes(bytes / elapsed)}/s average</p>
					) : null}
				</details>
			) : null}
		</div>
	);
}

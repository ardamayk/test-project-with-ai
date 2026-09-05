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

export function ImportTransferDetails({
	entry,
	isExpanded = false,
}: {
	entry: ImportFileEntry;
	isExpanded?: boolean;
}) {
	const [now, setNow] = useState(Date.now);
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
			{!isExpanded && entry.phase === "retry_wait" ? (
				<p>
					Retrying in{" "}
					{Math.max(0, Math.ceil(((entry.retryAt ?? now) - now) / 1000))}s ·{" "}
					{entry.retryCount}/3
				</p>
			) : null}
			{!isExpanded && entry.phase === "uploading" ? (
				<p>
					{formatBytes(bytes)} / {formatBytes(entry.file.size)}
				</p>
			) : null}
			{isExpanded && entry.startedAt ? (
				<div>
					<p>
						{entry.state === "unresolved"
							? `${elapsed}s elapsed`
							: `${formatBytes(entry.file.size)} file`}{" "}
						· {entry.retryCount ?? 0} retries
					</p>
					{entry.phase === "uploading" && elapsed > 0 ? (
						<p>{formatBytes(bytes / elapsed)}/s average</p>
					) : null}
				</div>
			) : null}
		</div>
	);
}

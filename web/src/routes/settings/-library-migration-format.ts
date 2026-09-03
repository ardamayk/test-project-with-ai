export function formatMigrationBytes(bytes: number): string {
	if (bytes === 0) return "0 bytes";
	return `${(bytes / 1024 / 1024).toFixed(2)} MiB`;
}

/**
 * Server-supplied text wins: structured reasons already carry the actionable
 * message, including the MusicBrainz Picard remediation hint.
 */
export function migrationFileReason(file: {
	errorReason?: string;
	errorCode?: string;
	errorField?: string;
}): string {
	const reason = file.errorReason ?? file.errorCode ?? "";
	if (file.errorField && file.errorCode && !file.errorReason)
		return `${file.errorCode} (${file.errorField})`;
	return reason;
}

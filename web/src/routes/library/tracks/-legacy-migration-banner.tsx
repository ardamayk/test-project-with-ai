import type { Track } from "@repo/api-client";
import { Link } from "@tanstack/react-router";

/**
 * Non-blocking notice shown while the loaded Track page still contains Legacy
 * Tracks; it only points at the Settings section that owns the migration.
 */
export function LegacyMigrationBanner({ tracks }: { tracks: Track[] }) {
	const legacyCount = tracks.filter(
		(track) => track.sourceKind === "legacy",
	).length;
	if (legacyCount === 0) return null;
	return (
		<div
			role="note"
			data-testid="legacy-migration-banner"
			className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border bg-card/40 px-4 py-3 text-sm"
		>
			<p className="text-foreground">
				{legacyCount} Legacy Track{legacyCount === 1 ? "" : "s"} still play from
				the old music folder. Migrate them into Managed Storage to enable
				replacement and deletion.
			</p>
			<Link
				to="/settings"
				className="rounded-md border border-border px-3 py-1.5 font-medium text-foreground hover:bg-accent"
			>
				Open Library Migration
			</Link>
		</div>
	);
}

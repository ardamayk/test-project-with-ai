import type { Track } from "@repo/api-client";
import { AlbumArt } from "@repo/ui";
import { apiClient } from "#/lib/api";
import { cn } from "#/lib/utils";

function hash(value: string): number {
	let out = 0;
	for (let i = 0; i < value.length; i += 1) {
		out = (out * 31 + value.charCodeAt(i)) >>> 0;
	}
	return out;
}

export function pickPreviewTracks<T extends { id: string }>(
	tracks: T[],
	seed: string,
	limit = 4,
): T[] {
	const next = [...tracks];
	let state = hash(seed) || 1;
	for (let i = next.length - 1; i > 0; i -= 1) {
		state = (state * 1664525 + 1013904223) >>> 0;
		const j = state % (i + 1);
		[next[i], next[j]] = [next[j], next[i]];
	}
	return next.slice(0, limit);
}

export function CollectionCoverStrip({
	tracks,
	seed,
	layout = "row",
	className,
}: {
	tracks: Track[];
	seed: string;
	layout?: "row" | "grid";
	className?: string;
}) {
	const preview = pickPreviewTracks(tracks, seed);
	const isGrid = layout === "grid";

	return (
		<div
			className={cn(
				isGrid
					? "grid size-32 shrink-0 grid-cols-2 gap-1 overflow-hidden rounded-lg bg-muted p-1 shadow-lg"
					: "flex w-fit shrink-0 items-center gap-1",
				className,
			)}
		>
			{preview.map((track) => (
				<AlbumArt
					key={track.id}
					coverUrl={apiClient.getAlbumCoverUrl(track.albumId)}
					title={track.title}
					className={cn(
						isGrid
							? "size-full rounded-md text-sm"
							: "size-10 rounded-md text-xs",
					)}
				/>
			))}
		</div>
	);
}

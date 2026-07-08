import type { Track } from "@repo/api-client";
import { AlbumArt } from "@repo/ui";
import { useMemo } from "react";
import { apiClient } from "#/lib/api";
import { cn } from "#/lib/utils";

const STACK_COVER_LIMIT = 4;

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

export function CollectionCoverStack({
	tracks,
	className,
}: {
	tracks: Track[];
	className?: string;
}) {
	const preview = useMemo(() => pickRandomUniqueAlbumTracks(tracks), [tracks]);
	const rotations = ["-rotate-10", "-rotate-3", "rotate-4", "rotate-10"];
	const offsets = [
		"left-[2%] top-[16%] z-10 h-[72%] w-[72%]",
		"left-[15%] top-[6%] z-20 h-[74%] w-[74%]",
		"left-[28%] top-[15%] z-30 h-[72%] w-[72%]",
		"left-[14%] top-[24%] z-40 h-[76%] w-[76%]",
	];

	return (
		<div
			data-testid="collection-cover-stack"
			className={cn(
				"relative h-56 w-56 shrink-0 sm:h-full sm:min-h-56",
				className,
			)}
		>
			{preview.map((track, index) => (
				<AlbumArt
					key={`${track.albumId}-${track.id}`}
					coverUrl={apiClient.getAlbumCoverUrl(track.albumId)}
					title={track.title}
					className={cn(
						"absolute rounded-md object-cover text-sm shadow-xl",
						offsets[index] ?? offsets.at(-1),
						rotations[index] ?? "rotate-0",
					)}
				/>
			))}
		</div>
	);
}

export function CollectionCoverRowStack({
	tracks,
	className,
}: {
	tracks: Track[];
	className?: string;
}) {
	const preview = useMemo(() => pickRandomUniqueAlbumTracks(tracks), [tracks]);
	const offsets = [
		"left-0",
		"left-[3.75rem]",
		"left-[7.5rem]",
		"left-[11.25rem]",
	];

	return (
		<div
			data-testid="playlist-row-cover-stack"
			className={cn("relative h-[7.5rem] w-[18.75rem] shrink-0", className)}
		>
			{preview.map((track, index) => (
				<AlbumArt
					key={`${track.albumId}-${track.id}`}
					coverUrl={apiClient.getAlbumCoverUrl(track.albumId)}
					title={track.title}
					className={cn(
						"absolute top-0 size-[7.5rem] rounded-md border border-background object-cover text-sm shadow-md",
						offsets[index] ?? offsets.at(-1),
					)}
				/>
			))}
		</div>
	);
}

export function pickRandomUniqueAlbumTracks(
	tracks: Track[],
	limit = STACK_COVER_LIMIT,
	random: () => number = Math.random,
): Track[] {
	const seenAlbumIds = new Set<string>();
	const uniqueTracks: Track[] = [];
	for (const track of tracks) {
		if (seenAlbumIds.has(track.albumId)) continue;
		seenAlbumIds.add(track.albumId);
		uniqueTracks.push(track);
	}

	for (let i = uniqueTracks.length - 1; i > 0; i -= 1) {
		const j = Math.floor(random() * (i + 1));
		[uniqueTracks[i], uniqueTracks[j]] = [uniqueTracks[j], uniqueTracks[i]];
	}

	return uniqueTracks.slice(0, limit);
}

export function CollectionCover({
	track,
	className,
}: {
	track: Track;
	className?: string;
}) {
	return (
		<AlbumArt
			coverUrl={apiClient.getAlbumCoverUrl(track.albumId)}
			title={track.title}
			className={cn("shrink-0 rounded-md text-xs", className)}
		/>
	);
}

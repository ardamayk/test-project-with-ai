import type { Track } from "@repo/api-client";
import { formatReplayGainAvailability, usePlayback } from "@repo/ui";
import { Clock, Heart, Info, ListMinus, Trash2, X } from "lucide-react";
import { Dialog as DialogPrimitive } from "radix-ui";
import { useState } from "react";
import {
	ContextMenu,
	ContextMenuContent,
	ContextMenuItem,
	ContextMenuTrigger,
} from "#/components/ui/context-menu";
import { confirmDelete, useDeleteTrack } from "#/hooks/use-delete-library";
import { useFavoriteTracks } from "#/hooks/use-favorite-tracks";
import { getTrackArtistName, getTrackGenreNames } from "#/lib/library-display";
import { cn } from "#/lib/utils";

function formatDuration(ms: number): string {
	if (!ms || ms < 0) return "0:00";
	const total = Math.floor(ms / 1000);
	const m = Math.floor(total / 60);
	const s = total % 60;
	return `${m}:${s.toString().padStart(2, "0")}`;
}

function formatDurationLabel(ms?: number): string | null {
	if (!ms || ms <= 0) return null;
	const total = Math.floor(ms / 1000);
	const minutes = Math.floor(total / 60);
	const seconds = total % 60;
	return `${minutes}m ${seconds}s`;
}

function formatSampleRate(hz?: number): string | null {
	if (!hz || hz <= 0) return null;
	if (hz % 1000 === 0) return `${hz / 1000} kHz`;
	return `${(hz / 1000).toFixed(1)} kHz`;
}

function formatBytes(bytes?: number): string | null {
	if (!bytes || bytes <= 0) return null;
	const mib = bytes / 1024 / 1024;
	return `${mib.toFixed(2)} MiB`;
}

export function TrackList({
	tracks,
	contextTracks,
	albumId,
	playMode = "single",
	showFavorite = false,
	showDelete = true,
	compact = false,
	numbering = "track",
	onRemoveTrack,
	onDeleteTrackSuccess,
	removeLabel = "Remove",
}: {
	tracks: Track[];
	contextTracks?: Track[];
	albumId?: string;
	playMode?: "single" | "double";
	showFavorite?: boolean;
	showMeta?: boolean;
	showDelete?: boolean;
	compact?: boolean;
	numbering?: "track" | "list";
	onRemoveTrack?: (track: Track) => void;
	onDeleteTrackSuccess?: (track: Track) => void;
	removeLabel?: string;
}) {
	const { playTrack, currentTrack, getAlbumCoverUrl } = usePlayback();
	const { isFavorite, toggleFavorite } = useFavoriteTracks();
	const deleteTrack = useDeleteTrack();
	const [detailsTrack, setDetailsTrack] = useState<Track | null>(null);

	const handlePlay = (track: Track) => {
		const queueTrackIds = (contextTracks ?? tracks).map((t) => t.id);
		void playTrack(track.id, queueTrackIds);
	};

	const handleDelete = (track: Track) => {
		const confirmed = confirmDelete(
			`Delete "${track.title}"?\n\nThis removes the track from your library and deletes its file from disk.`,
		);
		if (!confirmed) return;
		deleteTrack.mutate(track.id, {
			onSuccess: () => onDeleteTrackSuccess?.(track),
		});
	};

	const rowPadding = compact ? "px-3 py-1.5" : "px-3 py-2.5";
	const favoritePadding = compact ? "px-2 py-1.5" : "px-2 py-2.5";
	const shouldShowArtistLine = !albumId;
	const shouldShowAlbumCover = !albumId;
	const hasMultipleDiscs = tracks.some((track) => track.discNo > 1);

	return (
		<>
			<table className={cn("w-full", compact ? "text-xs" : "text-sm")}>
				<thead>
					<tr className="border-border border-b text-left text-caption text-[11px]">
						<th className={cn("w-10 font-medium", rowPadding)}>#</th>
						<th className={cn("font-medium", rowPadding)}>Title</th>
						<th className={cn("w-16 text-right font-medium", rowPadding)}>
							<span className="sr-only">Duration</span>
							<Clock className="ml-auto size-3.5" />
						</th>
						{showFavorite ? (
							<th
								className={cn("w-10 text-center font-medium", favoritePadding)}
							>
								<span className="sr-only">Favorite</span>
								<Heart className="mx-auto size-3.5" />
							</th>
						) : null}
					</tr>
				</thead>
				<tbody>
					{tracks.map((track, index) => {
						const isPlaying = currentTrack?.id === track.id;
						const favorited = isFavorite(track.id);
						const trackNumber = track.trackNo ?? index + 1;
						const visibleNumber =
							numbering === "list"
								? index + 1
								: hasMultipleDiscs
									? `${track.discNo}.${trackNumber}`
									: trackNumber;

						return (
							<ContextMenu key={track.id}>
								<ContextMenuTrigger asChild>
									<tr
										className={cn(
											"group cursor-pointer border-border/40 border-b transition hover:bg-muted/50",
											isPlaying && "bg-primary/5",
										)}
										tabIndex={0}
										onClick={() => {
											if (playMode === "single") handlePlay(track);
										}}
										onDoubleClick={() => {
											if (playMode === "double") handlePlay(track);
										}}
										onKeyDown={(event) => {
											if (event.key === "Enter") handlePlay(track);
										}}
									>
										<td className={cn("text-caption tabular-nums", rowPadding)}>
											{visibleNumber}
										</td>
										<td className={rowPadding}>
											<div className="flex min-w-0 items-center gap-2">
												{shouldShowAlbumCover ? (
													<img
														alt=""
														src={getAlbumCoverUrl(track.albumId)}
														className="size-8 shrink-0 rounded object-cover text-xs"
													/>
												) : null}
												<div className="min-w-0">
													<span
														className={cn(
															"block truncate font-medium leading-snug",
															compact ? "text-sm" : "text-base",
															isPlaying ? "text-heading" : "text-foreground",
														)}
													>
														{track.title}
													</span>
													{shouldShowArtistLine ? (
														<span className="mt-0.5 block truncate text-caption text-[11px] leading-tight">
															{getTrackArtistName(track)}
														</span>
													) : null}
												</div>
											</div>
										</td>
										<td
											className={cn(
												"text-right text-caption tabular-nums",
												rowPadding,
											)}
										>
											{formatDuration(track.durationMs)}
										</td>
										{showFavorite ? (
											<td className={cn("text-center", favoritePadding)}>
												<button
													type="button"
													aria-label={
														favorited
															? "Remove from favorites"
															: "Add to favorites"
													}
													className={cn(
														"inline-flex size-7 items-center justify-center rounded-full text-caption transition hover:bg-muted hover:text-heading",
														favorited && "text-heading",
													)}
													onClick={(event) => {
														event.stopPropagation();
														toggleFavorite(track.id);
													}}
												>
													<Heart
														className="size-3.5"
														fill={favorited ? "currentColor" : "none"}
													/>
												</button>
											</td>
										) : null}
									</tr>
								</ContextMenuTrigger>
								<ContextMenuContent>
									<ContextMenuItem onSelect={() => setDetailsTrack(track)}>
										<Info className="size-4" />
										Details
									</ContextMenuItem>
									{onRemoveTrack ? (
										<ContextMenuItem onSelect={() => onRemoveTrack(track)}>
											<ListMinus className="size-4" />
											{removeLabel}
										</ContextMenuItem>
									) : null}
									{showDelete ? (
										<ContextMenuItem
											variant="destructive"
											disabled={deleteTrack.isPending}
											onSelect={() => handleDelete(track)}
										>
											<Trash2 className="size-4" />
											Delete track
										</ContextMenuItem>
									) : null}
								</ContextMenuContent>
							</ContextMenu>
						);
					})}
				</tbody>
			</table>
			<TrackDetailsDialog
				track={detailsTrack}
				onOpenChange={(isOpen) => {
					if (!isOpen) setDetailsTrack(null);
				}}
			/>
		</>
	);
}

function TrackDetailsDialog({
	track,
	onOpenChange,
}: {
	track: Track | null;
	onOpenChange: (isOpen: boolean) => void;
}) {
	const rows = track
		? [
				["Title", track.title],
				["Artist", getTrackArtistName(track)],
				["Album", track.albumTitle],
				["Disc", track.discNo.toString()],
				["Track", track.trackNo?.toString()],
				["Duration", formatDurationLabel(track.durationMs)],
				["Codec", track.format],
				["Sample rate", formatSampleRate(track.sampleRateHz)],
				["Bit depth", track.bitDepth ? `${track.bitDepth}-bit` : null],
				[
					"Track ReplayGain",
					formatReplayGainAvailability(
						track.replayGain?.trackGainDb,
						track.replayGain?.trackPeak,
					),
				],
				[
					"Album ReplayGain",
					formatReplayGainAvailability(
						track.replayGain?.albumGainDb,
						track.replayGain?.albumPeak,
					),
				],
				["Genre", getTrackGenreNames(track).join(", ")],
				["Size", formatBytes(track.sizeBytes)],
				["Id", track.id],
			].filter((row): row is [string, string] => Boolean(row[1]))
		: [];

	return (
		<DialogPrimitive.Root open={Boolean(track)} onOpenChange={onOpenChange}>
			<DialogPrimitive.Portal>
				<DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-background/70" />
				<DialogPrimitive.Content className="-translate-x-1/2 -translate-y-1/2 fixed top-1/2 left-1/2 z-50 max-h-[80vh] w-[calc(100vw-2rem)] max-w-2xl overflow-hidden rounded-lg border border-border bg-popover text-popover-foreground shadow-xl outline-none">
					<div className="flex items-center justify-between gap-3 border-border border-b p-4">
						<DialogPrimitive.Title className="truncate font-semibold text-heading text-xl">
							{track?.title}
						</DialogPrimitive.Title>
						<DialogPrimitive.Close className="inline-flex size-8 items-center justify-center rounded-full hover:bg-muted">
							<span className="sr-only">Close</span>
							<X className="size-4" />
						</DialogPrimitive.Close>
					</div>
					{rows.length > 0 ? (
						<div className="max-h-[65vh] overflow-auto p-4">
							{rows.map(([label, value]) => (
								<DetailRow key={label} label={label} value={value} />
							))}
						</div>
					) : null}
				</DialogPrimitive.Content>
			</DialogPrimitive.Portal>
		</DialogPrimitive.Root>
	);
}

function DetailRow({ label, value }: { label: string; value?: string | null }) {
	if (!value) return null;
	return (
		<div className="grid grid-cols-[8rem_minmax(0,1fr)] gap-3 border-border border-b py-2 text-sm">
			<span className="text-caption">{label}</span>
			<span className="min-w-0 break-words font-medium text-foreground">
				{value}
			</span>
		</div>
	);
}

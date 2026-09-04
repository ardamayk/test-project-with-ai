import type { Track } from "@repo/api-client";
import { formatReplayGainAvailability, usePlayback } from "@repo/ui";
import {
	Clock,
	Heart,
	Info,
	ListMinus,
	Replace,
	Trash2,
	X,
} from "lucide-react";
import { Dialog as DialogPrimitive } from "radix-ui";
import { useRef, useState } from "react";
import {
	ContextMenu,
	ContextMenuContent,
	ContextMenuItem,
	ContextMenuTrigger,
} from "#/components/ui/context-menu";
import { useFavoriteTracks } from "#/hooks/use-favorite-tracks";
import { useReturnFocus } from "#/hooks/use-return-focus";
import { useServerCapability } from "#/hooks/use-server-capability";
import { useTrackDeletionFlow } from "#/hooks/use-track-deletion-flow";
import { useTrackReplacementFlow } from "#/hooks/use-track-replacement-flow";
import { getTrackArtistName, getTrackGenreNames } from "#/lib/library-display";
import { cn } from "#/lib/utils";
import { TrackDeletionDialog } from "./track-deletion-dialog";
import { TrackReplacementDialog } from "./track-replacement-dialog";

const MANAGED_TRACK_DELETION_CAPABILITY = "managed-track-deletion.v1";
const MANAGED_TRACK_REPLACEMENT_CAPABILITY = "managed-track-replacement.v1";

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

function formatBitDepth(bits?: number): string | null {
	if (!bits || bits <= 0) return null;
	return `${bits}-bit`;
}

function formatBitrate(kbps?: number, format?: string): string | null {
	if (!kbps || kbps <= 0) return null;
	const provenance =
		format?.toLowerCase() === "wav" ? "Native" : "Calculated by app";
	return `${kbps} kbps (${provenance})`;
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
	onReplaceTrackSuccess,
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
	onReplaceTrackSuccess?: (track: Track) => void;
	removeLabel?: string;
}) {
	const { playTrack, currentTrack, getAlbumCoverUrl } = usePlayback();
	const { isFavorite, toggleFavorite } = useFavoriteTracks();
	const trackDeletion = useTrackDeletionFlow(onDeleteTrackSuccess);
	const hasDeletionCapability = useServerCapability(
		MANAGED_TRACK_DELETION_CAPABILITY,
	);
	const trackReplacement = useTrackReplacementFlow(onReplaceTrackSuccess);
	const hasReplacementCapability = useServerCapability(
		MANAGED_TRACK_REPLACEMENT_CAPABILITY,
	);
	const [detailsTrack, setDetailsTrack] = useState<Track | null>(null);
	// Context menu items unmount with the menu, so dialogs opened from them
	// return focus to the originating row (or the table once a row is gone).
	const rowRefs = useRef(new Map<string, HTMLTableRowElement>());
	const tableRef = useRef<HTMLTableElement>(null);
	const returnFocus = useReturnFocus();
	const captureRowFocus = (trackId: string) =>
		returnFocus.capture(rowRefs.current.get(trackId));
	const restoreRowFocus = (event: Event) =>
		returnFocus.restore(event, tableRef.current);

	const handlePlay = (track: Track) => {
		const queueTrackIds = (contextTracks ?? tracks).map((t) => t.id);
		void playTrack(track.id, queueTrackIds);
	};

	const rowPadding = compact ? "px-3 py-1.5" : "px-3 py-2.5";
	const favoritePadding = compact ? "px-2 py-1.5" : "px-2 py-2.5";
	const shouldShowArtistLine = !albumId;
	const shouldShowAlbumCover = !albumId;
	const hasMultipleDiscs = tracks.some((track) => track.discNo > 1);

	return (
		<>
			<table
				ref={tableRef}
				tabIndex={-1}
				className={cn("w-full outline-none", compact ? "text-xs" : "text-sm")}
			>
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
										ref={(row) => {
											if (row) rowRefs.current.set(track.id, row);
											else rowRefs.current.delete(track.id);
										}}
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
									<ContextMenuItem
										onSelect={() => {
											captureRowFocus(track.id);
											setDetailsTrack(track);
										}}
									>
										<Info className="size-4" />
										Details
									</ContextMenuItem>
									{onRemoveTrack ? (
										<ContextMenuItem onSelect={() => onRemoveTrack(track)}>
											<ListMinus className="size-4" />
											{removeLabel}
										</ContextMenuItem>
									) : null}
									{showDelete && hasReplacementCapability ? (
										<ContextMenuItem
											disabled={trackReplacement.isBusy}
											onSelect={() => {
												captureRowFocus(track.id);
												trackReplacement.open(track);
											}}
										>
											<Replace className="size-4" />
											Replace file
										</ContextMenuItem>
									) : null}
									{showDelete && hasDeletionCapability ? (
										<ContextMenuItem
											variant="destructive"
											disabled={trackDeletion.isDeleting}
											onSelect={() => {
												captureRowFocus(track.id);
												trackDeletion.open(track);
											}}
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
				onCloseAutoFocus={restoreRowFocus}
			/>
			<TrackDeletionDialog
				track={trackDeletion.track}
				onCloseAutoFocus={restoreRowFocus}
				preview={trackDeletion.preview}
				error={trackDeletion.error}
				isLoading={trackDeletion.isLoading}
				isDeleting={trackDeletion.isDeleting}
				onCancel={trackDeletion.cancel}
				onConfirm={trackDeletion.confirm}
			/>
			<TrackReplacementDialog
				track={trackReplacement.track}
				onCloseAutoFocus={restoreRowFocus}
				step={trackReplacement.step}
				preview={trackReplacement.preview}
				progress={trackReplacement.progress}
				error={trackReplacement.error}
				isBusy={trackReplacement.isBusy}
				isDesktop={trackReplacement.isDesktop}
				onCancel={() => void trackReplacement.cancel()}
				onClose={trackReplacement.close}
				onFile={(file) => void trackReplacement.replaceWith(file)}
				onSelectDesktopFile={() => void trackReplacement.selectDesktopFile()}
				onConfirm={() => void trackReplacement.confirm()}
			/>
		</>
	);
}

function TrackDetailsDialog({
	track,
	onOpenChange,
	onCloseAutoFocus,
}: {
	track: Track | null;
	onOpenChange: (isOpen: boolean) => void;
	onCloseAutoFocus?: (event: Event) => void;
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
				["Bitrate", formatBitrate(track.bitrateKbps, track.format)],
				["Sample rate", formatSampleRate(track.sampleRateHz)],
				["Bit depth", formatBitDepth(track.bitDepth)],
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
				<DialogPrimitive.Content
					onCloseAutoFocus={onCloseAutoFocus}
					className="-translate-x-1/2 -translate-y-1/2 fixed top-1/2 left-1/2 z-50 max-h-[80vh] w-[calc(100vw-2rem)] max-w-2xl overflow-hidden rounded-lg border border-border bg-popover text-popover-foreground shadow-xl outline-none"
				>
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

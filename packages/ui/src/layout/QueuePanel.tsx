import type { QueueItem } from "@repo/api-client";
import { ListMusic, X } from "lucide-react";
import { type RefObject, useEffect, useRef } from "react";
import { cn } from "../lib/utils";
import { usePlayback } from "../playback/PlaybackProvider";
import { getQueuePanel } from "../widgets/layout-utils";
import { AlbumArt } from "./AlbumArt";
import { useLayout } from "./LayoutProvider";
import { PanelCollapseButton } from "./PanelCollapseButton";

function formatDuration(ms: number): string {
	if (!ms || ms < 0) return "0:00";
	const total = Math.floor(ms / 1000);
	const m = Math.floor(total / 60);
	const s = total % 60;
	return `${m}:${s.toString().padStart(2, "0")}`;
}

export function QueuePanel({
	embedded = false,
}: {
	/** Rendered inside another surface (Lyrics view): no collapse control or suggestion footer. */
	embedded?: boolean;
} = {}) {
	const { preferences, togglePanel } = useLayout();
	const scrollContainerRef = useRef<HTMLDivElement>(null);
	const panelSide = getQueuePanel(preferences.layout.sidebarPosition);
	const isCollapsed = !embedded && preferences.layout.collapsed[panelSide];

	const {
		queue,
		queueConflict,
		currentTrack,
		playbackSource,
		playQueueIndex,
		removeFromQueue,
		clearQueue,
		getAlbumCoverUrl,
	} = usePlayback();
	const currentIndex = findCurrentQueueIndex(
		queue,
		playbackSource?.type === "track" ? playbackSource.queueItemId : undefined,
		currentTrack?.id,
	);

	if (isCollapsed) {
		return (
			<div className="flex h-full flex-col items-center bg-queue text-queue-foreground">
				<div className="flex w-full justify-center px-1 pt-2">
					<PanelCollapseButton
						edge={panelSide}
						collapsed
						onToggle={() => togglePanel(panelSide)}
					/>
				</div>
				<div
					className="relative mt-3 flex size-9 items-center justify-center rounded-md text-caption"
					title={`Queue${queue.length > 0 ? ` (${queue.length})` : ""}`}
				>
					<ListMusic className="size-4" />
					{queue.length > 0 ? (
						<span className="absolute -top-1 -right-1 flex size-4 items-center justify-center rounded-full bg-primary font-medium text-[0.625rem] text-primary-foreground">
							{queue.length > 9 ? "9+" : queue.length}
						</span>
					) : null}
				</div>
			</div>
		);
	}

	return (
		<div className="flex h-full flex-col">
			<div
				className={cn(
					"flex items-center justify-between gap-2 border-border border-b px-3 py-2",
					panelSide === "left" && "flex-row-reverse",
				)}
			>
				<div className="flex min-w-0 flex-1 items-center gap-2">
					<h2 className="font-semibold text-base">Queue</h2>
					{queue.length > 0 ? (
						<button
							type="button"
							className="text-caption text-xs hover:text-foreground"
							onClick={() => void clearQueue()}
						>
							Clear all
						</button>
					) : null}
				</div>
				{embedded ? null : (
					<PanelCollapseButton
						edge={panelSide}
						collapsed={false}
						onToggle={() => togglePanel(panelSide)}
					/>
				)}
			</div>
			<div ref={scrollContainerRef} className="flex-1 overflow-y-auto p-3">
				{queueConflict ? (
					<p
						role="alert"
						className="mb-3 rounded-md border border-destructive/40 bg-destructive/10 p-2 text-destructive text-xs"
					>
						{queueConflict}
					</p>
				) : null}
				{queue.length === 0 ? (
					<p className="text-foreground text-sm">Queue is empty</p>
				) : (
					<QueueSections
						scrollContainerRef={scrollContainerRef}
						queue={queue}
						currentIndex={currentIndex}
						getAlbumCoverUrl={getAlbumCoverUrl}
						onPlay={(index) => void playQueueIndex(index)}
						onRemove={(itemId) => void removeFromQueue(itemId)}
					/>
				)}
			</div>
			{embedded ? null : (
				<div className="border-border border-t p-3">
					<div className="rounded-lg border border-dashed border-border bg-muted/30 p-3">
						<p className="font-medium text-heading text-xs">Smart suggestion</p>
						<p className="mt-1 text-caption text-xs">
							Based on your listening — coming soon.
						</p>
					</div>
				</div>
			)}
		</div>
	);
}

/** Prefer the Queue item the engine reports; fall back to the first item whose Track matches. */
function findCurrentQueueIndex(
	queue: QueueItem[],
	queueItemId: string | undefined,
	trackId: string | undefined,
): number {
	if (queueItemId) {
		const byItem = queue.findIndex((item) => item.id === queueItemId);
		if (byItem >= 0) return byItem;
	}
	if (trackId) return queue.findIndex((item) => item.track.id === trackId);
	return -1;
}

type QueueRowProps = {
	item: QueueItem;
	state: "played" | "current" | "upcoming";
	coverUrl: string;
	onPlay: () => void;
	onRemove: () => void;
};

function QueueSections({
	scrollContainerRef,
	queue,
	currentIndex,
	getAlbumCoverUrl,
	onPlay,
	onRemove,
}: {
	scrollContainerRef: RefObject<HTMLDivElement | null>;
	queue: QueueItem[];
	currentIndex: number;
	getAlbumCoverUrl: (albumId: string) => string;
	onPlay: (index: number) => void;
	onRemove: (itemId: string) => void;
}) {
	const currentRowRef = useRef<HTMLLIElement>(null);
	const currentItemId = currentIndex >= 0 ? queue[currentIndex]?.id : null;

	useEffect(() => {
		if (!currentItemId) return;
		const row = currentRowRef.current;
		const viewport = scrollContainerRef.current;
		if (!row || !viewport) return;
		const rowBounds = row.getBoundingClientRect();
		const viewportBounds = viewport.getBoundingClientRect();
		const offset =
			rowBounds.top < viewportBounds.top
				? rowBounds.top - viewportBounds.top
				: Math.max(0, rowBounds.bottom - viewportBounds.bottom);
		// scrollIntoView also scrolls ancestors, including the shell when the
		// closed drawer is translated below it. Only move the queue viewport.
		if (offset !== 0) viewport.scrollBy?.({ top: offset, behavior: "smooth" });
	}, [currentItemId, scrollContainerRef]);

	const renderRow = (item: QueueItem, index: number) => {
		const state =
			currentIndex < 0 || index > currentIndex
				? "upcoming"
				: index === currentIndex
					? "current"
					: "played";
		return (
			<QueueRow
				key={item.id}
				ref={state === "current" ? currentRowRef : undefined}
				item={item}
				state={state}
				coverUrl={getAlbumCoverUrl(item.track.albumId)}
				onPlay={() => onPlay(index)}
				onRemove={() => onRemove(item.id)}
			/>
		);
	};

	if (currentIndex < 0) {
		return (
			<section aria-label="Next up">
				<QueueSectionHeading>Next up</QueueSectionHeading>
				<ul className="flex flex-col gap-1">{queue.map(renderRow)}</ul>
			</section>
		);
	}

	const played = queue.slice(0, currentIndex);
	const upcoming = queue.slice(currentIndex + 1);

	return (
		<div className="flex flex-col gap-4">
			{played.length > 0 ? (
				<section aria-label="Played">
					<QueueSectionHeading>Played</QueueSectionHeading>
					<ul className="flex flex-col gap-1">
						{played.map((item, index) => renderRow(item, index))}
					</ul>
				</section>
			) : null}
			<section aria-label="Playing">
				<QueueSectionHeading>Playing</QueueSectionHeading>
				<ul className="flex flex-col gap-1">
					{renderRow(queue[currentIndex], currentIndex)}
				</ul>
			</section>
			{upcoming.length > 0 ? (
				<section aria-label="Next up">
					<QueueSectionHeading>Next up</QueueSectionHeading>
					<ul className="flex flex-col gap-1">
						{upcoming.map((item, index) =>
							renderRow(item, currentIndex + 1 + index),
						)}
					</ul>
				</section>
			) : null}
		</div>
	);
}

function QueueSectionHeading({ children }: { children: string }) {
	return (
		<p className="mb-1.5 px-2 font-semibold text-[0.6875rem] text-caption uppercase tracking-wide">
			{children}
		</p>
	);
}

function QueueRow({
	ref,
	item,
	state,
	coverUrl,
	onPlay,
	onRemove,
}: QueueRowProps & { ref?: React.Ref<HTMLLIElement> }) {
	const isCurrent = state === "current";
	return (
		<li
			ref={ref}
			data-queue-state={state}
			aria-current={isCurrent ? "true" : undefined}
			className={cn(
				"group flex cursor-pointer items-center gap-2 rounded-lg px-2 py-2 transition-colors",
				state === "played" && "opacity-55 hover:bg-muted/50 hover:opacity-100",
				state === "upcoming" && "hover:bg-muted/50",
				isCurrent &&
					"bg-[var(--player-pill)] shadow-[0_0_0_1px_var(--player-control-primary),0_6px_18px_-6px_var(--player-control-shadow)] ring-1 ring-[var(--player-control-primary)]/40",
			)}
			onClick={onPlay}
			onKeyDown={(event) => {
				if (event.key === "Enter" || event.key === " ") {
					event.preventDefault();
					onPlay();
				}
			}}
			role="button"
			tabIndex={0}
		>
			<AlbumArt
				coverUrl={coverUrl}
				title={item.track.title}
				className={cn(
					"size-8 shrink-0 rounded text-xs",
					isCurrent && "size-10",
				)}
			/>
			<div className="min-w-0 flex-1 text-left">
				<p
					className={cn(
						"truncate text-sm",
						isCurrent
							? "font-semibold text-[var(--player-control-primary)]"
							: "text-foreground",
					)}
				>
					{item.track.title}
				</p>
				<p className="truncate text-foreground text-xs">
					{item.track.artistName}
				</p>
			</div>
			<span className="shrink-0 text-caption text-xs tabular-nums">
				{formatDuration(item.track.durationMs)}
			</span>
			<button
				type="button"
				className="inline-flex size-5 shrink-0 items-center justify-center rounded text-caption hover:text-foreground"
				onClick={(event) => {
					event.stopPropagation();
					onRemove();
				}}
				aria-label="Remove from queue"
			>
				<X className="size-3.5" />
			</button>
		</li>
	);
}

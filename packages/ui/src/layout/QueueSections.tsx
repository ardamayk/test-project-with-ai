import type { QueueItem } from "@repo/api-client";
import { ListMusic, Plus, Sparkles } from "lucide-react";
import { type CSSProperties, type RefObject, useEffect, useRef } from "react";
import { AlbumArt } from "./AlbumArt";
import { useQueueNavigation } from "./QueueRowMenuProvider";
import { QueueTrackRow } from "./QueueTrackRow";
import {
	getQueueGroupDetails,
	groupQueueItems,
	type QueueGroup,
} from "./queue-groups";
import { useQueueSourceTitles } from "./use-queue-source-titles";

export function QueueSections({
	scrollContainerRef,
	queue,
	currentIndex,
	isPlaying,
	isUpdating,
	getAlbumCoverUrl,
	onPlay,
	onDismiss,
}: {
	scrollContainerRef: RefObject<HTMLDivElement | null>;
	queue: QueueItem[];
	currentIndex: number;
	isPlaying: boolean;
	isUpdating: boolean;
	getAlbumCoverUrl: (albumId: string) => string;
	onPlay: (index: number) => void;
	onDismiss: (items: QueueItem[]) => void;
}) {
	const sourceTitles = useQueueSourceTitles(queue);
	const currentRowRef = useRef<HTMLLIElement>(null);
	const currentItemId = queue[currentIndex]?.id;
	useEffect(() => {
		const row = currentRowRef.current;
		const viewport = scrollContainerRef.current;
		if (!currentItemId || !row || !viewport) return;
		const rowBounds = row.getBoundingClientRect();
		const viewportBounds = viewport.getBoundingClientRect();
		const offset =
			rowBounds.top < viewportBounds.top
				? rowBounds.top - viewportBounds.top
				: Math.max(0, rowBounds.bottom - viewportBounds.bottom);
		// Scroll only this viewport: the closed drawer is translated below the shell.
		const shouldReduceMotion = window.matchMedia?.(
			"(prefers-reduced-motion: reduce)",
		).matches;
		if (offset !== 0)
			viewport.scrollBy?.({
				top: offset,
				behavior: shouldReduceMotion ? "instant" : "smooth",
			});
	}, [currentItemId, scrollContainerRef]);

	return groupQueueItems(queue).map((group) => {
		const details = getQueueGroupDetails(group, queue, sourceTitles);
		return (
			<section
				key={group.items[0].id}
				aria-label={details.title}
				className="mb-2 overflow-hidden rounded-lg last:mb-0"
				style={{ "--queue-group-color": details.color } as CSSProperties}
			>
				<QueueGroupHeader
					sourceTitles={sourceTitles}
					group={group}
					queue={queue}
					coverUrl={getAlbumCoverUrl(group.items[0].track.albumId)}
					isUpdating={isUpdating}
					onDismiss={() => onDismiss(group.items)}
				/>
				<ul className="flex flex-col gap-px border-[var(--queue-group-color)] border-l-2 py-0.5">
					{group.items.map((item, index) => {
						const queueIndex = group.startIndex + index;
						return (
							<QueueTrackRow
								key={item.id}
								ref={queueIndex === currentIndex ? currentRowRef : undefined}
								item={item}
								number={index + 1}
								isAlbum={group.source.kind === "album"}
								state={
									queueIndex === currentIndex
										? "current"
										: queueIndex < currentIndex
											? "played"
											: "upcoming"
								}
								isPlaying={isPlaying}
								coverUrl={getAlbumCoverUrl(item.track.albumId)}
								onPlay={() => onPlay(queueIndex)}
							/>
						);
					})}
				</ul>
			</section>
		);
	});
}

function QueueGroupHeader({
	sourceTitles,
	group,
	queue,
	coverUrl,
	isUpdating,
	onDismiss,
}: {
	sourceTitles: Record<string, string | null>;
	group: QueueGroup;
	queue: QueueItem[];
	coverUrl: string;
	isUpdating: boolean;
	onDismiss: () => void;
}) {
	const details = getQueueGroupDetails(group, queue, sourceTitles);
	const onNavigate = useQueueNavigation();
	const Icon =
		group.source.kind === "suggestion"
			? Sparkles
			: group.source.kind === "playlist"
				? ListMusic
				: Plus;
	const label = (
		<>
			{group.source.kind === "album" ? (
				<AlbumArt
					coverUrl={coverUrl}
					title={details.title}
					className="size-7 shrink-0 rounded text-[10px]"
				/>
			) : (
				<span className="flex size-7 shrink-0 items-center justify-center text-[var(--queue-group-color)]">
					<Icon className="size-3.5" />
				</span>
			)}
			<span className="min-w-0 flex-1 truncate font-semibold text-heading text-sm leading-5">
				{details.title}
			</span>
		</>
	);
	return (
		<div
			className="flex items-center gap-2 border-[var(--queue-group-color)] border-l-2 bg-[var(--player-pill)] py-2 pr-3 pl-3"
			title={details.meta}
		>
			{details.href ? (
				<a
					href={details.href}
					onClick={(event) => {
						if (
							onNavigate &&
							details.href &&
							event.button === 0 &&
							!event.metaKey &&
							!event.ctrlKey &&
							!event.shiftKey &&
							!event.altKey
						) {
							event.preventDefault();
							onNavigate(details.href);
						}
					}}
					aria-label={`${details.title}: ${details.meta}`}
					className="flex min-w-0 flex-1 items-center gap-2 rounded outline-offset-2 hover:underline"
				>
					{label}
				</a>
			) : (
				<div
					className="flex min-w-0 flex-1 items-center gap-2"
					title={details.meta}
				>
					{label}
				</div>
			)}
			<span className="text-xs text-caption tabular-nums">
				{group.items.length}
			</span>
			{group.source.kind === "suggestion" && (
				<button
					type="button"
					disabled={isUpdating}
					onClick={onDismiss}
					className="h-5 shrink-0 rounded-[5px] border border-[var(--sidebar-border)] px-[7px] text-[11px] text-caption hover:text-heading disabled:opacity-40"
				>
					Not now
				</button>
			)}
		</div>
	);
}

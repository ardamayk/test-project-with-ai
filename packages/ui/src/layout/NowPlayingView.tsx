import type { Track } from "@repo/api-client";
import { ChevronDown, MicVocal } from "lucide-react";
import { type CSSProperties, useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { useFocusTrap } from "../lib/use-focus-trap";
import { cn } from "../lib/utils";
import { usePlayback } from "../playback/PlaybackProvider";
import { AlbumArt } from "./AlbumArt";
import { PlaybackControls } from "./PlayerBarControls";
import { QueuePanel } from "./QueuePanel";

/**
 * Full-screen "Now Playing" view: a blurred cover backdrop, large art, the
 * transport and the Queue. Sibling of the Lyrics view and opened from the
 * cover in the Player Bar.
 */
export function NowPlayingView({
	track,
	coverUrl,
	accentStyle,
	onOpenLyrics,
	onClose,
}: {
	track: Track;
	coverUrl: string | null;
	/** Cover-derived custom properties, forwarded so the view matches the bar. */
	accentStyle?: CSSProperties;
	onOpenLyrics?: () => void;
	onClose: () => void;
}) {
	const rootRef = useRef<HTMLDivElement>(null);
	useFocusTrap(rootRef);
	const {
		isPlaying,
		currentTime,
		duration,
		bufferedEnd,
		shuffleEnabled,
		repeatMode,
		togglePlay,
		toggleShuffle,
		cycleRepeatMode,
		navigatePrevious,
		navigateNext,
		seek,
	} = usePlayback();

	useEffect(() => {
		const closeOnEscape = (event: KeyboardEvent) => {
			if (event.key === "Escape") onClose();
		};
		document.addEventListener("keydown", closeOnEscape);
		return () => document.removeEventListener("keydown", closeOnEscape);
	}, [onClose]);

	if (typeof document === "undefined") return null;

	const effectiveDuration =
		duration > 0
			? duration
			: track.durationMs > 0
				? track.durationMs / 1000
				: 0;

	return createPortal(
		<div
			ref={rootRef}
			role="dialog"
			aria-modal="true"
			aria-label="Now playing"
			tabIndex={-1}
			style={accentStyle}
			className="lyrics-view-enter fixed inset-0 z-[60] flex flex-col overflow-hidden bg-background text-foreground outline-none"
		>
			{coverUrl ? (
				<img
					src={coverUrl}
					alt=""
					aria-hidden
					data-testid="now-playing-backdrop"
					// A tiny blurred copy scaled up: cheap on WebKitGTK and still
					// reads as the album's palette.
					className="pointer-events-none absolute inset-0 h-full w-full scale-125 object-cover opacity-35 blur-3xl saturate-150"
				/>
			) : null}
			<div className="pointer-events-none absolute inset-0 bg-gradient-to-b from-background/40 via-background/70 to-background" />
			<header className="relative flex shrink-0 items-center justify-between gap-4 px-6 py-3">
				<p className="text-caption text-xs uppercase tracking-[0.2em]">
					Now playing
				</p>
				<button
					type="button"
					className="inline-flex size-9 items-center justify-center rounded-full text-foreground hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
					aria-label="Close now playing"
					onClick={onClose}
				>
					<ChevronDown className="size-5" />
				</button>
			</header>
			<div className="relative flex min-h-0 flex-1">
				<section
					aria-label="Current track"
					className="flex min-w-0 flex-1 flex-col items-center justify-center gap-8 px-8 pb-10"
				>
					<AlbumArt
						coverUrl={coverUrl}
						title={track.title}
						className="size-[min(52vh,26rem)] rounded-2xl text-4xl shadow-[0_30px_80px_-20px_rgba(0,0,0,0.6)]"
					/>
					<div className="w-full max-w-xl text-center">
						<h1 className="truncate font-semibold text-2xl text-heading">
							{track.title}
						</h1>
						<p className="mt-1 truncate text-base text-caption">
							{track.artistName}
							{track.albumTitle ? ` · ${track.albumTitle}` : ""}
						</p>
					</div>
					<div className="w-full max-w-xl">
						<PlaybackControls
							isRadioPlaying={false}
							isPlaying={isPlaying}
							hasPlayableSource
							hasCurrentTrack
							currentTime={currentTime}
							effectiveDuration={effectiveDuration}
							bufferedEnd={bufferedEnd}
							shuffleEnabled={shuffleEnabled}
							repeatMode={repeatMode}
							onTogglePlay={togglePlay}
							onToggleShuffle={toggleShuffle}
							onCycleRepeatMode={cycleRepeatMode}
							onPrevious={navigatePrevious}
							onNext={navigateNext}
							onSeek={seek}
						/>
					</div>
					{onOpenLyrics ? (
						<button
							type="button"
							className={cn(
								"inline-flex items-center gap-2 rounded-full border border-border px-4 py-1.5 text-sm hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40",
							)}
							onClick={onOpenLyrics}
						>
							<MicVocal className="size-4" aria-hidden />
							Lyrics
						</button>
					) : null}
				</section>
				<aside className="hidden w-[22rem] shrink-0 border-border border-l bg-queue/80 text-queue-foreground backdrop-blur md:block">
					<QueuePanel embedded />
				</aside>
			</div>
		</div>,
		document.body,
	);
}

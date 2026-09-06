import {
	AudioLines,
	Disc3,
	Infinity as InfinityIcon,
	ListMusic,
	Pause,
	Play,
	Repeat,
	Shuffle,
	SkipBack,
	SkipForward,
	Volume1,
	Volume2,
	VolumeX,
} from "lucide-react";
import {
	type CSSProperties,
	type ReactNode,
	useEffect,
	useRef,
	useState,
} from "react";
import { cn } from "../lib/utils";
import type { RepeatMode } from "../playback/PlaybackProvider";

const CONTROL_BUTTON_CLASS =
	"inline-flex size-5 items-center justify-center rounded text-player-foreground hover:text-[var(--player-control-primary)] disabled:opacity-40";
const ACTIVE_CONTROL_BUTTON_CLASS = "text-[var(--player-control-primary)]";

type PlaybackControlsProps = {
	isRadioPlaying: boolean;
	isPlaying: boolean;
	hasPlayableSource: boolean;
	hasCurrentTrack: boolean;
	currentTime: number;
	effectiveDuration: number;
	shuffleEnabled: boolean;
	repeatMode: RepeatMode;
	onTogglePlay: () => void;
	onToggleShuffle: () => void;
	onCycleRepeatMode: () => void;
	onPrevious: () => void;
	onNext: () => void;
	onSeek: (seconds: number) => void;
};

export function PlaybackControls(props: PlaybackControlsProps) {
	return (
		<section
			aria-label="Playback controls"
			className="flex min-w-px max-w-[448px] flex-[1_0_0] flex-col items-center justify-center justify-self-center"
		>
			<TransportControls {...props} />
			<PlaybackProgress {...props} />
		</section>
	);
}

function TransportControls({
	isPlaying,
	hasPlayableSource,
	hasCurrentTrack,
	shuffleEnabled,
	repeatMode,
	onTogglePlay,
	onToggleShuffle,
	onCycleRepeatMode,
	onPrevious,
	onNext,
}: PlaybackControlsProps) {
	return (
		<div className="flex items-center gap-6">
			<ShuffleButton
				isEnabled={shuffleEnabled}
				disabled={!hasCurrentTrack}
				onClick={onToggleShuffle}
			/>
			<QueueNavigationButton
				label="Previous"
				disabled={!hasCurrentTrack}
				onPlay={onPrevious}
			/>
			<PrimaryPlaybackButton
				isPlaying={isPlaying}
				disabled={!hasPlayableSource}
				onClick={onTogglePlay}
			/>
			<QueueNavigationButton
				label="Next"
				disabled={!hasCurrentTrack}
				onPlay={onNext}
			/>
			<RepeatButton
				repeatMode={repeatMode}
				disabled={!hasCurrentTrack}
				onClick={onCycleRepeatMode}
			/>
		</div>
	);
}

function QueueNavigationButton({
	label,
	disabled,
	onPlay,
}: {
	label: "Previous" | "Next";
	disabled: boolean;
	onPlay: () => void;
}) {
	const Icon = label === "Previous" ? SkipBack : SkipForward;
	return (
		<button
			type="button"
			className={CONTROL_BUTTON_CLASS}
			onClick={() => {
				if (!disabled) onPlay();
			}}
			disabled={disabled}
			aria-label={label}
		>
			<Icon className="size-4" />
		</button>
	);
}

function ShuffleButton({
	isEnabled,
	disabled,
	onClick,
}: {
	isEnabled: boolean;
	disabled: boolean;
	onClick: () => void;
}) {
	return (
		<button
			type="button"
			className={cn(
				CONTROL_BUTTON_CLASS,
				isEnabled && ACTIVE_CONTROL_BUTTON_CLASS,
			)}
			aria-label={isEnabled ? "Shuffle on" : "Shuffle off"}
			onClick={onClick}
			disabled={disabled}
		>
			<Shuffle className="size-4" />
		</button>
	);
}

function PrimaryPlaybackButton({
	isPlaying,
	disabled,
	onClick,
}: {
	isPlaying: boolean;
	disabled: boolean;
	onClick: () => void;
}) {
	return (
		<button
			type="button"
			className="inline-flex size-10 items-center justify-center rounded-xl bg-[var(--player-control-primary)] text-[var(--player-control-primary-foreground)] shadow-[0px_10px_15px_-3px_var(--player-control-shadow),0px_4px_6px_-4px_var(--player-control-shadow)] hover:opacity-90 disabled:opacity-50"
			onClick={onClick}
			disabled={disabled}
			aria-label={isPlaying ? "Pause" : "Play"}
		>
			{isPlaying ? <Pause className="size-4" /> : <Play className="size-4" />}
		</button>
	);
}

function RepeatButton({
	repeatMode,
	disabled,
	onClick,
}: {
	repeatMode: RepeatMode;
	disabled: boolean;
	onClick: () => void;
}) {
	const label =
		repeatMode === "off"
			? "Repeat off"
			: repeatMode === "once"
				? "Repeat once"
				: "Repeat loop";
	return (
		<button
			type="button"
			className={cn(
				CONTROL_BUTTON_CLASS,
				repeatMode !== "off" && ACTIVE_CONTROL_BUTTON_CLASS,
				"relative",
			)}
			aria-label={label}
			onClick={onClick}
			disabled={disabled}
		>
			<Repeat className="size-4" />
			{repeatMode === "once" ? (
				<span className="-right-0.5 -bottom-0.5 absolute flex size-3 items-center justify-center rounded-full bg-primary font-semibold text-[0.5rem] text-primary-foreground">
					1
				</span>
			) : null}
			{repeatMode === "loop" ? (
				<span
					className="-right-1 -bottom-1 absolute flex size-4 items-center justify-center rounded-full bg-primary text-primary-foreground"
					role="img"
					aria-label="Repeat infinitely"
				>
					<InfinityIcon className="size-2.5" />
				</span>
			) : null}
		</button>
	);
}

function PlaybackProgress({
	isRadioPlaying,
	hasCurrentTrack,
	currentTime,
	effectiveDuration,
	onSeek,
}: {
	isRadioPlaying: boolean;
	hasCurrentTrack: boolean;
	currentTime: number;
	effectiveDuration: number;
	onSeek: (seconds: number) => void;
}) {
	return (
		<div className="mt-2 flex w-full min-w-0 items-center gap-2 text-[11px] tabular-nums">
			<span className="w-8 shrink-0 text-right text-player-foreground">
				{isRadioPlaying ? "LIVE" : formatTime(currentTime)}
			</span>
			{isRadioPlaying ? (
				<div className="h-1 min-w-0 flex-1 rounded-full bg-[var(--player-live-progress)]/45" />
			) : (
				<input
					type="range"
					min={0}
					max={1}
					step={0.001}
					value={effectiveDuration > 0 ? currentTime / effectiveDuration : 0}
					onChange={(event) =>
						effectiveDuration > 0 &&
						onSeek(Number(event.target.value) * effectiveDuration)
					}
					className="h-1 min-w-0 flex-1 accent-[var(--player-live-progress)] disabled:opacity-100"
					disabled={!hasCurrentTrack}
					aria-label="Seek"
				/>
			)}
			<span className="w-8 shrink-0 text-player-foreground">
				{isRadioPlaying ? "--:--" : formatTime(effectiveDuration)}
			</span>
		</div>
	);
}

type VolumeAndQueueControlsProps = {
	qualityLabel: string;
	isLossless: boolean;
	volume: number;
	signalControl?: ReactNode;
	onToggleQueue: () => void;
	onVolumeChange: (value: number) => void;
};

export function VolumeAndQueueControls({
	qualityLabel,
	isLossless,
	volume,
	signalControl,
	onToggleQueue,
	onVolumeChange,
}: VolumeAndQueueControlsProps) {
	const QualityIcon = isLossless ? Disc3 : AudioLines;
	return (
		<section
			aria-label="Volume and queue"
			className="flex min-w-[150px] flex-[1_0_0] items-center justify-end gap-4 justify-self-end"
		>
			{signalControl}
			<button
				type="button"
				className="inline-flex h-6 shrink-0 items-center gap-2 rounded-xl border border-[var(--sidebar-border)] bg-[var(--player-pill)] px-[13px] py-[5px] text-[11px] text-player-foreground disabled:opacity-100"
				aria-label={`Quality ${qualityLabel}`}
				disabled
				title="Quality selector coming soon"
			>
				<QualityIcon className="size-3 shrink-0" />
				<span className="hidden font-medium tabular-nums md:inline">
					{qualityLabel}
				</span>
			</button>
			<VolumeControl volume={volume} onVolumeChange={onVolumeChange} />
			<button
				type="button"
				className="hidden size-5 shrink-0 items-center justify-center rounded text-player-foreground hover:text-[var(--player-control-primary)] sm:inline-flex"
				onClick={onToggleQueue}
				aria-label="Toggle queue panel"
			>
				<ListMusic className="size-4" />
			</button>
		</section>
	);
}

function VolumeControl({
	volume,
	onVolumeChange,
}: {
	volume: number;
	onVolumeChange: (value: number) => void;
}) {
	const VolumeIcon = volume <= 0 ? VolumeX : volume < 0.5 ? Volume1 : Volume2;
	// Touch devices have no hover, so a tap opens the popover instead of muting.
	const [touchOpen, setTouchOpen] = useState(false);
	const lastPointerWasTouchRef = useRef(false);
	const lastAudibleVolumeRef = useRef(volume > 0 ? volume : 1);
	if (volume > 0) lastAudibleVolumeRef.current = volume;

	const rootRef = useRef<HTMLDivElement>(null);
	useEffect(() => {
		if (!touchOpen) return undefined;
		const closeOnOutsideTap = (event: PointerEvent) => {
			if (!rootRef.current?.contains(event.target as Node)) setTouchOpen(false);
		};
		document.addEventListener("pointerdown", closeOnOutsideTap);
		return () => document.removeEventListener("pointerdown", closeOnOutsideTap);
	}, [touchOpen]);

	const toggleMute = () =>
		onVolumeChange(volume <= 0 ? lastAudibleVolumeRef.current : 0);

	return (
		<div ref={rootRef} className="group relative flex shrink-0 items-center">
			<button
				type="button"
				className={cn(
					CONTROL_BUTTON_CLASS,
					"group-focus-within:text-[var(--player-control-primary)] group-hover:text-[var(--player-control-primary)]",
				)}
				aria-label={volume <= 0 ? "Unmute" : "Mute"}
				onPointerDown={(event) => {
					lastPointerWasTouchRef.current = event.pointerType === "touch";
				}}
				onClick={() => {
					if (lastPointerWasTouchRef.current) {
						setTouchOpen((open) => !open);
						return;
					}
					toggleMute();
				}}
			>
				<VolumeIcon className="size-4" />
			</button>
			<div
				data-testid="volume-popover"
				className={cn(
					"pointer-events-none absolute bottom-full left-1/2 z-40 -translate-x-1/2 pb-3 opacity-0 transition-opacity duration-150 group-focus-within:pointer-events-auto group-focus-within:opacity-100 group-hover:pointer-events-auto group-hover:opacity-100",
					touchOpen && "pointer-events-auto opacity-100",
				)}
			>
				<div className="flex h-32 w-9 items-center justify-center rounded-xl border border-[var(--player-border)] bg-player px-2 py-3 shadow-[0_8px_24px_-4px_var(--player-shadow)]">
					<div className="relative h-full w-4">
						<input
							type="range"
							min={0}
							max={1}
							step={0.01}
							value={volume}
							onChange={(event) => onVolumeChange(Number(event.target.value))}
							className="player-volume-slider"
							style={
								{
									"--volume-level": `${Math.round(volume * 100)}%`,
								} as CSSProperties
							}
							aria-label="Volume"
							aria-orientation="vertical"
						/>
					</div>
				</div>
			</div>
		</div>
	);
}

function formatTime(seconds: number): string {
	if (!Number.isFinite(seconds) || seconds < 0) return "0:00";
	const minutes = Math.floor(seconds / 60);
	const remainingSeconds = Math.floor(seconds % 60);
	return `${minutes}:${remainingSeconds.toString().padStart(2, "0")}`;
}

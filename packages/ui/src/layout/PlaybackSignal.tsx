import { Check, ChevronUp } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { cn } from "../lib/utils";
import type { OutputMode } from "../playback/processing";

type PlaybackSignalProps = {
	outputMode: OutputMode;
	outputControls: PlaybackOutputControls;
};

export type PlaybackOutputControls = {
	selectNormalOutput(): void;
	selectExclusiveOutput(): void;
	enableAdaptiveSystemRate(): void;
};

const OUTPUT_OPTIONS: Array<{ mode: OutputMode; label: string }> = [
	{ mode: "system", label: "Normal" },
	{ mode: "direct-alsa", label: "Exclusive" },
	{ mode: "adaptive-system-rate", label: "Adaptive" },
];

export function PlaybackSignal({
	outputMode,
	outputControls,
}: PlaybackSignalProps) {
	const containerRef = useRef<HTMLDivElement>(null);
	const [isOpen, setIsOpen] = useState(false);
	const activeLabel = getOutputLabel(outputMode);

	const closeMenu = useCallback(() => {
		setIsOpen(false);
	}, []);

	useEffect(() => {
		if (!isOpen) return;
		const closeOnOutsidePointer = (event: MouseEvent) => {
			if (!containerRef.current?.contains(event.target as Node)) closeMenu();
		};
		const closeOnEscape = (event: KeyboardEvent) => {
			if (event.key === "Escape") closeMenu();
		};
		document.addEventListener("mousedown", closeOnOutsidePointer);
		document.addEventListener("keydown", closeOnEscape);
		return () => {
			document.removeEventListener("mousedown", closeOnOutsidePointer);
			document.removeEventListener("keydown", closeOnEscape);
		};
	}, [closeMenu, isOpen]);

	const selectMode = (mode: OutputMode) => {
		if (mode === "system") outputControls.selectNormalOutput();
		if (mode === "direct-alsa") outputControls.selectExclusiveOutput();
		if (mode === "adaptive-system-rate")
			outputControls.enableAdaptiveSystemRate();
		closeMenu();
	};

	return (
		<div ref={containerRef} className="relative">
			<button
				type="button"
				aria-label={`Output mode: ${activeLabel}`}
				aria-haspopup="menu"
				aria-expanded={isOpen}
				className="inline-flex h-6 items-center gap-1 rounded-xl border border-[var(--sidebar-border)] bg-[var(--player-pill)] px-2.5 text-[11px] text-player-foreground hover:text-[var(--player-control-primary)]"
				onClick={() => setIsOpen((value) => !value)}
			>
				{activeLabel}
				<ChevronUp className="size-3" aria-hidden />
			</button>
			{isOpen ? (
				<div
					role="menu"
					aria-label="Output mode"
					className="absolute right-0 bottom-full z-50 mb-1.5 w-32 overflow-hidden rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-xl"
				>
					{OUTPUT_OPTIONS.map((option) => (
						<button
							type="button"
							role="menuitemradio"
							aria-checked={outputMode === option.mode}
							key={option.mode}
							className={cn(
								"flex h-7 w-full items-center gap-1.5 rounded-sm px-1.5 text-left text-xs hover:bg-muted",
								outputMode === option.mode &&
									"bg-muted text-[var(--player-control-primary)]",
							)}
							onClick={() => selectMode(option.mode)}
						>
							<span className="inline-flex size-3 shrink-0 items-center justify-center">
								{outputMode === option.mode ? (
									<Check className="size-3" aria-hidden />
								) : null}
							</span>
							{option.label}
						</button>
					))}
				</div>
			) : null}
		</div>
	);
}

function getOutputLabel(outputMode: OutputMode) {
	return (
		OUTPUT_OPTIONS.find((option) => option.mode === outputMode)?.label ??
		"Normal"
	);
}

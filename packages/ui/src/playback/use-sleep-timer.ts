import { useCallback, useEffect, useState } from "react";
import type { PlaybackEngine, PlaybackSessionState } from "./PlaybackEngine";

export type SleepTimerMode =
	| { kind: "off" }
	| { kind: "after-track" }
	| { kind: "minutes"; minutes: number; endsAt: number };

export type SleepTimerRequest =
	| { kind: "off" }
	| { kind: "after-track" }
	| { kind: "minutes"; minutes: number };

export type SleepTimer = {
	mode: SleepTimerMode;
	/** Seconds left for a minutes timer; null otherwise. */
	remainingSeconds: number | null;
	setSleepTimer: (request: SleepTimerRequest) => void;
};

const TICK_MS = 1000;

/**
 * Sleep timer above the engine. "After this track" is delegated to the
 * engine's stop-after-current flag so a native engine that owns queue
 * advancement honours it; a minutes timer simply pauses when it elapses.
 */
export function useSleepTimer(
	engine: PlaybackEngine,
	session: PlaybackSessionState,
	now: () => number = Date.now,
): SleepTimer {
	const [minutesTimer, setMinutesTimer] = useState<{
		minutes: number;
		endsAt: number;
	} | null>(null);
	const [remainingSeconds, setRemainingSeconds] = useState<number | null>(null);

	useEffect(() => {
		if (!minutesTimer) {
			setRemainingSeconds(null);
			return undefined;
		}
		const tick = () => {
			const remaining = Math.max(
				0,
				Math.ceil((minutesTimer.endsAt - now()) / 1000),
			);
			setRemainingSeconds(remaining);
			if (remaining === 0) {
				engine.pause();
				setMinutesTimer(null);
			}
		};
		tick();
		const interval = window.setInterval(tick, TICK_MS);
		return () => window.clearInterval(interval);
	}, [engine, minutesTimer, now]);

	const setSleepTimer = useCallback(
		(request: SleepTimerRequest) => {
			setMinutesTimer(null);
			engine.setStopAfterCurrent?.(request.kind === "after-track");
			if (request.kind === "minutes") {
				setMinutesTimer({
					minutes: request.minutes,
					endsAt: now() + request.minutes * 60_000,
				});
			}
		},
		[engine, now],
	);

	const mode: SleepTimerMode = minutesTimer
		? { kind: "minutes", ...minutesTimer }
		: session.stopAfterCurrent
			? { kind: "after-track" }
			: { kind: "off" };

	return { mode, remainingSeconds, setSleepTimer };
}

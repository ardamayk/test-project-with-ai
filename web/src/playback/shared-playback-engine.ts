import type { PlaybackEngine } from "@repo/ui";
import { createPlaybackEngine } from "./create-playback-engine";

type PlaybackEngineFactory = () => PlaybackEngine;

let sharedPlaybackEngine: PlaybackEngine | null = null;

export function getSharedPlaybackEngine(
	createEngine: PlaybackEngineFactory = createPlaybackEngine,
): PlaybackEngine {
	if (!sharedPlaybackEngine) {
		sharedPlaybackEngine = createEngine();
	}
	return sharedPlaybackEngine;
}

export function disposeSharedPlaybackEngine(): void {
	sharedPlaybackEngine?.destroy();
	sharedPlaybackEngine = null;
}

if (import.meta.hot) {
	import.meta.hot.dispose(disposeSharedPlaybackEngine);
}

import type {
	PlaybackEngine,
	PlaybackSessionListener,
	PlaybackSessionState,
	PlaybackSource,
} from "@repo/ui";

export class DesktopSourceRoutingPlaybackEngine implements PlaybackEngine {
	private readonly listeners = new Set<PlaybackSessionListener>();
	private readonly unsubscribeNative: () => void;
	private browserEngine: PlaybackEngine | null = null;
	private unsubscribeBrowser: (() => void) | null = null;
	private activeEngine: PlaybackEngine;
	private isDestroyed = false;

	constructor(
		private readonly nativeEngine: PlaybackEngine,
		private readonly createBrowserEngine: () => PlaybackEngine,
	) {
		this.activeEngine = nativeEngine;
		this.unsubscribeNative = nativeEngine.subscribe((state) => {
			this.publish(nativeEngine, state);
		});
	}

	getState() {
		return this.activeEngine.getState();
	}

	subscribe(listener: PlaybackSessionListener) {
		this.listeners.add(listener);
		return () => this.listeners.delete(listener);
	}

	async play(source?: PlaybackSource) {
		const targetEngine = source ? this.getEngineFor(source) : this.activeEngine;
		if (targetEngine !== this.activeEngine) {
			const previousEngine = this.activeEngine;
			this.activeEngine = targetEngine;
			previousEngine.stop();
		}
		await targetEngine.play(source);
	}

	pause() {
		this.activeEngine.pause();
	}

	stop() {
		this.activeEngine.stop();
	}

	togglePlay() {
		this.activeEngine.togglePlay();
	}

	seek(seconds: number) {
		this.activeEngine.seek(seconds);
	}

	setVolume(value: number) {
		this.activeEngine.setVolume(value);
	}

	toggleShuffle() {
		this.activeEngine.toggleShuffle();
	}

	cycleRepeatMode() {
		this.activeEngine.cycleRepeatMode();
	}

	destroy() {
		this.isDestroyed = true;
		this.unsubscribeNative();
		this.unsubscribeBrowser?.();
		this.nativeEngine.destroy();
		this.browserEngine?.destroy();
		this.listeners.clear();
	}

	private getEngineFor(source: PlaybackSource) {
		if (source.type === "track") return this.nativeEngine;
		if (this.browserEngine) return this.browserEngine;
		this.browserEngine = this.createBrowserEngine();
		this.unsubscribeBrowser = this.browserEngine.subscribe((state) => {
			this.publish(this.browserEngine, state);
		});
		return this.browserEngine;
	}

	private publish(engine: PlaybackEngine | null, state: PlaybackSessionState) {
		if (this.isDestroyed || engine !== this.activeEngine) return;
		for (const listener of this.listeners) listener(state);
	}
}

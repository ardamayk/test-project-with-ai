import {
	DEFAULT_PLAYBACK_SESSION_STATE,
	type PlaybackEngine,
	type PlaybackNavigationDirection,
	type PlaybackNavigationListener,
	type PlaybackSessionListener,
	type PlaybackSessionState,
	type PlaybackSource,
} from "../PlaybackEngine";

export class InMemoryPlaybackEngine implements PlaybackEngine {
	private state: PlaybackSessionState = { ...DEFAULT_PLAYBACK_SESSION_STATE };
	private readonly listeners = new Set<PlaybackSessionListener>();
	private readonly navigationListeners = new Set<PlaybackNavigationListener>();

	getState() {
		return this.state;
	}

	subscribe(listener: PlaybackSessionListener) {
		this.listeners.add(listener);
		return () => this.listeners.delete(listener);
	}

	subscribeNavigation(listener: PlaybackNavigationListener) {
		this.navigationListeners.add(listener);
		return () => this.navigationListeners.delete(listener);
	}

	navigate(direction: PlaybackNavigationDirection) {
		for (const listener of this.navigationListeners) listener(direction);
	}

	async play(source?: PlaybackSource) {
		const nextSource = source ?? this.state.source;
		if (!nextSource) return;
		this.update({
			source: nextSource,
			status: "playing",
			currentTime: source ? 0 : this.state.currentTime,
			duration:
				nextSource.type === "track" && nextSource.track.durationMs > 0
					? nextSource.track.durationMs / 1000
					: 0,
			error: null,
		});
	}

	pause() {
		if (this.state.source) this.update({ status: "paused" });
	}

	stop() {
		this.update({
			source: null,
			status: "idle",
			currentTime: 0,
			duration: 0,
			error: null,
		});
	}

	togglePlay() {
		if (this.state.status === "playing") {
			this.pause();
			return;
		}
		void this.play();
	}

	seek(seconds: number) {
		this.update({ currentTime: seconds });
	}

	setVolume(value: number) {
		this.update({ volume: Math.min(1, Math.max(0, value)) });
	}

	toggleShuffle() {
		this.update({ shuffleEnabled: !this.state.shuffleEnabled });
	}

	cycleRepeatMode() {
		const repeatMode =
			this.state.repeatMode === "off"
				? "once"
				: this.state.repeatMode === "once"
					? "loop"
					: "off";
		this.update({ repeatMode });
	}

	finish() {
		if (this.state.repeatMode === "once") {
			this.update({ currentTime: 0, status: "playing", repeatMode: "off" });
			return;
		}
		if (this.state.repeatMode === "loop") {
			this.update({ currentTime: 0, status: "playing" });
			return;
		}
		this.update({ status: "ended" });
	}

	destroy() {
		this.listeners.clear();
	}

	private update(next: Partial<PlaybackSessionState>) {
		this.state = { ...this.state, ...next };
		for (const listener of this.listeners) listener(this.state);
	}
}

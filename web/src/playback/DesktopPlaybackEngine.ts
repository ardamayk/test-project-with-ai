import type {
	PlaybackEngine,
	PlaybackSessionListener,
	PlaybackSessionState,
	PlaybackSource,
} from "@repo/ui";
import { DEFAULT_PLAYBACK_SESSION_STATE } from "@repo/ui";
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";

const PLAYBACK_STATE_EVENT = "desktop-playback-state";

type DesktopPlaybackBridge = {
	getState(): Promise<PlaybackSessionState>;
	play(source?: PlaybackSource): Promise<PlaybackSessionState>;
	pause(): Promise<PlaybackSessionState>;
	stop(): Promise<PlaybackSessionState>;
	togglePlay(): Promise<PlaybackSessionState>;
	seek(seconds: number): Promise<PlaybackSessionState>;
	setVolume(value: number): Promise<PlaybackSessionState>;
	toggleShuffle(): Promise<PlaybackSessionState>;
	cycleRepeatMode(): Promise<PlaybackSessionState>;
	listen(listener: PlaybackSessionListener): Promise<UnlistenFn>;
};

const tauriPlaybackBridge: DesktopPlaybackBridge = {
	getState: () => invoke("get_desktop_playback_state"),
	play: (source) => invoke("desktop_playback_play", { source }),
	pause: () => invoke("desktop_playback_pause"),
	stop: () => invoke("desktop_playback_stop"),
	togglePlay: () => invoke("desktop_playback_toggle_play"),
	seek: (seconds) => invoke("desktop_playback_seek", { seconds }),
	setVolume: (value) => invoke("desktop_playback_set_volume", { value }),
	toggleShuffle: () => invoke("desktop_playback_toggle_shuffle"),
	cycleRepeatMode: () => invoke("desktop_playback_cycle_repeat_mode"),
	listen: (listener) =>
		listen<PlaybackSessionState>(PLAYBACK_STATE_EVENT, (event) => {
			listener(event.payload);
		}),
};

export class DesktopPlaybackEngine implements PlaybackEngine {
	private state: PlaybackSessionState = { ...DEFAULT_PLAYBACK_SESSION_STATE };
	private readonly listeners = new Set<PlaybackSessionListener>();
	private unlisten: UnlistenFn | null = null;
	private isDestroyed = false;
	private commandRevision = 0;

	constructor(
		private readonly bridge: DesktopPlaybackBridge = tauriPlaybackBridge,
	) {
		void this.initialize(this.commandRevision);
	}

	getState() {
		return this.state;
	}

	subscribe(listener: PlaybackSessionListener) {
		this.listeners.add(listener);
		return () => this.listeners.delete(listener);
	}

	async play(source?: PlaybackSource) {
		this.commandRevision += 1;
		try {
			this.update(await this.bridge.play(source));
		} catch (error) {
			this.updateError(error);
			throw error;
		}
	}

	pause() {
		this.runCommand(() => this.bridge.pause());
	}

	stop() {
		this.runCommand(() => this.bridge.stop());
	}

	togglePlay() {
		this.runCommand(() => this.bridge.togglePlay());
	}

	seek(seconds: number) {
		this.runCommand(() => this.bridge.seek(seconds));
	}

	setVolume(value: number) {
		this.runCommand(() => this.bridge.setVolume(value));
	}

	toggleShuffle() {
		this.runCommand(() => this.bridge.toggleShuffle());
	}

	cycleRepeatMode() {
		this.runCommand(() => this.bridge.cycleRepeatMode());
	}

	destroy() {
		this.isDestroyed = true;
		this.unlisten?.();
		this.unlisten = null;
		this.listeners.clear();
	}

	private async initialize(initializationRevision: number) {
		try {
			const unlisten = await this.bridge.listen((state) => this.update(state));
			if (this.isDestroyed) {
				unlisten();
				return;
			}
			this.unlisten = unlisten;
			const state = await this.bridge.getState();
			if (this.commandRevision === initializationRevision) this.update(state);
		} catch (error) {
			this.updateError(error);
		}
	}

	private runCommand(command: () => Promise<PlaybackSessionState>) {
		this.commandRevision += 1;
		void this.run(command);
	}

	private async run(command: () => Promise<PlaybackSessionState>) {
		try {
			this.update(await command());
		} catch (error) {
			this.updateError(error);
		}
	}

	private update(state: PlaybackSessionState) {
		if (this.isDestroyed) return;
		this.state = state;
		for (const listener of this.listeners) listener(state);
	}

	private updateError(error: unknown) {
		this.update({
			...this.state,
			status: "error",
			error: {
				code: "playback-failed",
				message: getErrorMessage(error),
			},
		});
	}
}

function getErrorMessage(error: unknown) {
	if (error instanceof Error) return error.message;
	if (typeof error === "string") return error;
	if (
		typeof error === "object" &&
		error !== null &&
		"message" in error &&
		typeof error.message === "string"
	) {
		return error.message;
	}
	return "Native playback failed";
}

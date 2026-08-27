import type {
	PlaybackEngine,
	PlaybackNavigationListener,
	PlaybackSessionListener,
	PlaybackSessionState,
	PlaybackSource,
} from "@repo/ui";
import { DEFAULT_PLAYBACK_SESSION_STATE } from "@repo/ui";
import Hls from "hls.js";

export interface BrowserPlaybackMedia extends EventTarget {
	currentTime: number;
	duration: number;
	paused: boolean;
	src: string;
	volume: number;
	canPlayType(type: string): string;
	play(): Promise<void>;
	pause(): void;
	removeAttribute(name: string): void;
}

type BrowserHls = {
	attachMedia(media: BrowserPlaybackMedia): void;
	destroy(): void;
	loadSource(url: string): void;
};

type BrowserHlsFactory = {
	isSupported(): boolean;
	create(): BrowserHls;
};

type BrowserPlaybackEngineOptions = {
	createMedia?: () => BrowserPlaybackMedia;
	hls?: BrowserHlsFactory;
};

const defaultHlsFactory: BrowserHlsFactory = {
	isSupported: () => Hls.isSupported(),
	create: () => {
		const hls = new Hls();
		return {
			attachMedia: (media) => hls.attachMedia(media as HTMLMediaElement),
			destroy: () => hls.destroy(),
			loadSource: (url) => hls.loadSource(url),
		};
	},
};

export class BrowserPlaybackEngine implements PlaybackEngine {
	private readonly media: BrowserPlaybackMedia;
	private readonly hlsFactory: BrowserHlsFactory;
	private readonly listeners = new Set<PlaybackSessionListener>();
	private readonly navigationListeners = new Set<PlaybackNavigationListener>();
	private state: PlaybackSessionState = { ...DEFAULT_PLAYBACK_SESSION_STATE };
	private hls: BrowserHls | null = null;

	constructor(options: BrowserPlaybackEngineOptions = {}) {
		this.media = options.createMedia?.() ?? new Audio();
		this.hlsFactory = options.hls ?? defaultHlsFactory;
		this.media.volume = this.state.volume;
		this.addMediaListeners();
	}

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

	previous() {
		this.publishNavigation("previous");
	}

	next() {
		this.publishNavigation("next");
	}

	async play(source?: PlaybackSource) {
		const nextSource = source ?? this.state.source;
		if (!nextSource) return;

		if (source) {
			this.update({
				source,
				status: "paused",
				currentTime: 0,
				duration:
					source.type === "track" && source.track.durationMs > 0
						? source.track.durationMs / 1000
						: 0,
				error: null,
			});
		}

		try {
			if (source) this.setMediaSource(source);
			await this.media.play();
			this.update({ status: "playing", error: null });
		} catch (error) {
			this.update({
				status: "error",
				error: {
					code: "playback-failed",
					message: getErrorMessage(error),
				},
			});
			throw error;
		}
	}

	pause() {
		this.media.pause();
		if (this.state.source) this.update({ status: "paused" });
	}

	stop() {
		this.media.pause();
		this.destroyHls();
		this.media.removeAttribute("src");
		this.update({
			source: null,
			status: "idle",
			currentTime: 0,
			duration: 0,
			error: null,
		});
	}

	togglePlay() {
		if (this.media.paused) {
			void this.play().catch(() => undefined);
			return;
		}
		this.pause();
	}

	seek(seconds: number) {
		this.media.currentTime = seconds;
		this.update({ currentTime: seconds });
	}

	setVolume(value: number) {
		const volume = Math.min(1, Math.max(0, value));
		this.media.volume = volume;
		this.update({ volume });
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

	destroy() {
		this.media.pause();
		this.destroyHls();
		this.removeMediaListeners();
		this.media.removeAttribute("src");
		this.listeners.clear();
		this.navigationListeners.clear();
	}

	private publishNavigation(direction: "previous" | "next") {
		for (const listener of this.navigationListeners) listener(direction);
	}

	private readonly handleTimeUpdate = () => {
		this.update({ currentTime: this.media.currentTime });
	};

	private readonly handleDurationChange = () => {
		if (Number.isFinite(this.media.duration) && this.media.duration > 0) {
			this.update({ duration: this.media.duration });
		}
	};

	private readonly handlePlay = () => this.update({ status: "playing" });
	private readonly handlePause = () => {
		if (this.state.status !== "ended") this.update({ status: "paused" });
	};

	private readonly handleEnded = () => {
		if (this.state.repeatMode === "once" || this.state.repeatMode === "loop") {
			const repeatMode =
				this.state.repeatMode === "once" ? "off" : this.state.repeatMode;
			this.media.currentTime = 0;
			this.update({ currentTime: 0, status: "playing", repeatMode });
			void this.play().catch(() => undefined);
			return;
		}
		this.update({ status: "ended" });
	};

	private addMediaListeners() {
		this.media.addEventListener("timeupdate", this.handleTimeUpdate);
		this.media.addEventListener("durationchange", this.handleDurationChange);
		this.media.addEventListener("loadedmetadata", this.handleDurationChange);
		this.media.addEventListener("play", this.handlePlay);
		this.media.addEventListener("pause", this.handlePause);
		this.media.addEventListener("ended", this.handleEnded);
	}

	private removeMediaListeners() {
		this.media.removeEventListener("timeupdate", this.handleTimeUpdate);
		this.media.removeEventListener("durationchange", this.handleDurationChange);
		this.media.removeEventListener("loadedmetadata", this.handleDurationChange);
		this.media.removeEventListener("play", this.handlePlay);
		this.media.removeEventListener("pause", this.handlePause);
		this.media.removeEventListener("ended", this.handleEnded);
	}

	private setMediaSource(source: PlaybackSource) {
		this.destroyHls();
		const sourceUrl =
			source.type === "track" ? source.playbackUrl : source.sourceUrl;
		if (isHlsStream(sourceUrl) && this.hlsFactory.isSupported()) {
			this.hls = this.hlsFactory.create();
			this.hls.loadSource(source.playbackUrl);
			this.hls.attachMedia(this.media);
			return;
		}
		if (isHlsStream(sourceUrl) && canPlayNativeHls(this.media)) {
			this.media.src = source.playbackUrl;
			return;
		}
		this.media.src = source.playbackUrl;
	}

	private destroyHls() {
		this.hls?.destroy();
		this.hls = null;
	}

	private update(next: Partial<PlaybackSessionState>) {
		this.state = { ...this.state, ...next };
		for (const listener of this.listeners) listener(this.state);
	}
}

function isHlsStream(url: string) {
	try {
		return new URL(url, window.location.href).pathname
			.toLowerCase()
			.endsWith(".m3u8");
	} catch {
		return url.toLowerCase().includes(".m3u8");
	}
}

function canPlayNativeHls(media: BrowserPlaybackMedia) {
	return Boolean(
		media.canPlayType("application/vnd.apple.mpegurl") ||
			media.canPlayType("application/x-mpegurl"),
	);
}

function getErrorMessage(error: unknown) {
	return error instanceof Error ? error.message : "Playback failed";
}

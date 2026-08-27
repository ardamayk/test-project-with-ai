import type { RadioSearchResult, RadioStation, Track } from "@repo/api-client";

export type RepeatMode = "off" | "once" | "loop";

export type PlaybackSource =
	| {
			type: "track";
			track: Track;
			playbackUrl: string;
	  }
	| {
			type: "radio-station";
			station: RadioStation;
			playbackUrl: string;
			sourceUrl: string;
	  }
	| {
			type: "catalog-preview";
			result: RadioSearchResult;
			playbackUrl: string;
			sourceUrl: string;
	  };

export type PlaybackStatus = "idle" | "playing" | "paused" | "ended" | "error";

export type PlaybackError = {
	code: "playback-failed";
	message: string;
};

export type PlaybackSessionState = {
	source: PlaybackSource | null;
	status: PlaybackStatus;
	currentTime: number;
	duration: number;
	volume: number;
	shuffleEnabled: boolean;
	repeatMode: RepeatMode;
	error: PlaybackError | null;
};

export type PlaybackSessionListener = (state: PlaybackSessionState) => void;

export interface PlaybackEngine {
	getState(): PlaybackSessionState;
	subscribe(listener: PlaybackSessionListener): () => void;
	play(source?: PlaybackSource): Promise<void>;
	pause(): void;
	stop(): void;
	togglePlay(): void;
	seek(seconds: number): void;
	setVolume(value: number): void;
	toggleShuffle(): void;
	cycleRepeatMode(): void;
	destroy(): void;
}

export const DEFAULT_PLAYBACK_SESSION_STATE: PlaybackSessionState = {
	source: null,
	status: "idle",
	currentTime: 0,
	duration: 0,
	volume: 0.8,
	shuffleEnabled: false,
	repeatMode: "off",
	error: null,
};

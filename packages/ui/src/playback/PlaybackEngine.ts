import type { RadioSearchResult, RadioStation, Track } from "@repo/api-client";
import type {
	EqualizerPreset,
	OutputDevice,
	OutputDeviceIssue,
	OutputMode,
	ProcessingProfile,
	ProcessingState,
	ReplayGainMode,
} from "./processing";
import type { PlaybackTelemetry } from "./telemetry";

export type RepeatMode = "off" | "once" | "loop";

export type PlaybackSource =
	| {
			type: "track";
			track: Track;
			playbackUrl: string;
			queueItemId?: string;
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
	code:
		| "playback-failed"
		| "mpv-crash-loop"
		| "mpv-restart-failed"
		| "mpv-restart-unavailable"
		| "mpv-recovery-state-unavailable"
		| "mpv-recovery-snapshot-failed"
		| "mpv-recovery-lifecycle-unavailable"
		| "mpv-recovery-transition-invalid";
	message: string;
};

export type PlaybackSessionState = {
	source: PlaybackSource | null;
	outputMode: OutputMode | null;
	availableOutputDevices: OutputDevice[];
	selectedOutputDevice: OutputDevice | null;
	outputDeviceIssue: OutputDeviceIssue | null;
	status: PlaybackStatus;
	currentTime: number;
	duration: number;
	volume: number;
	shuffleEnabled: boolean;
	repeatMode: RepeatMode;
	error: PlaybackError | null;
	processing?: ProcessingState;
	telemetry?: PlaybackTelemetry;
};

export type PlaybackSessionListener = (state: PlaybackSessionState) => void;
export type PlaybackNavigationDirection = "previous" | "next";
export type PlaybackNavigationListener = (
	direction: PlaybackNavigationDirection,
) => void;

export interface PlaybackEngine {
	getState(): PlaybackSessionState;
	subscribe(listener: PlaybackSessionListener): () => void;
	subscribeNavigation?(listener: PlaybackNavigationListener): () => void;
	syncQueueContext?(
		sources: PlaybackSource[],
		currentIndex: number | null,
	): Promise<void>;
	play(source?: PlaybackSource): Promise<void>;
	previous(): void;
	next(): void;
	pause(): void;
	stop(): void;
	togglePlay(): void;
	seek(seconds: number): void;
	setVolume(value: number): void;
	setProcessingProfile?(profile: ProcessingProfile): void;
	setReplayGainMode?(mode: ReplayGainMode): void;
	setEqualizerPreset?(preset: Exclude<EqualizerPreset, "custom">): void;
	setEqualizerGain?(index: number, value: number): void;
	refreshOutputDevices?(): void;
	selectDirectAlsaOutput?(deviceId: string): void;
	selectExclusiveOutput?(): void;
	fallbackToSystemOutput?(): void;
	enableAdaptiveSystemRate?(isConfirmed: boolean): void;
	toggleShuffle(): void;
	cycleRepeatMode(): void;
	destroy(): void;
}

export const DEFAULT_PLAYBACK_SESSION_STATE: PlaybackSessionState = {
	source: null,
	outputMode: null,
	availableOutputDevices: [],
	selectedOutputDevice: null,
	outputDeviceIssue: null,
	status: "idle",
	currentTime: 0,
	duration: 0,
	volume: 0.8,
	shuffleEnabled: false,
	repeatMode: "off",
	error: null,
};

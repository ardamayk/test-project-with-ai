export const EQ_FREQUENCIES_HZ = [
	31.25, 62.5, 125, 250, 500, 1000, 2000, 4000, 8000, 16000,
] as const;

export type ProcessingProfile = "direct" | "processed";
export type ReplayGainMode = "off" | "track" | "album";
export type ReplayGainPreference = Exclude<ReplayGainMode, "off">;
export type EqualizerPreset =
	| "flat"
	| "bass-boost"
	| "vocal"
	| "treble-boost"
	| "custom";

export type EqualizerState = {
	isEnabled: boolean;
	preset: EqualizerPreset;
	gainsDb: number[];
};

export type ProcessingState = {
	profile: ProcessingProfile;
	softwareVolume: number;
	replayGainMode: ReplayGainMode;
	replayGainPreference: ReplayGainPreference | null;
	equalizer: EqualizerState;
	effectiveAudioFilters: string[];
	transitionNotice: string | null;
};

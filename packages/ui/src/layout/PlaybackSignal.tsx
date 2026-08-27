import type { Track } from "@repo/api-client";
import { Activity, X } from "lucide-react";
import { useState } from "react";
import {
	EQ_FREQUENCIES_HZ,
	type EqualizerPreset,
	type OutputMode,
	type ProcessingState,
	type ReplayGainPreference,
} from "../playback/processing";
import {
	derivePlaybackTelemetryStatus,
	deriveReplayGainAvailability,
	describePlaybackTelemetry,
	formatTelemetryStatus,
	mergeProcessingState,
	type PlaybackTelemetry,
	type PlaybackTelemetryDescriptions,
} from "../playback/telemetry";

type PlaybackSignalProps = {
	telemetry: PlaybackTelemetry;
	outputMode?: OutputMode;
	processingControls?: PlaybackProcessingControls;
	replayGainMetadata?: Track["replayGain"];
};

export type PlaybackProcessingControls = {
	state: ProcessingState;
	setProfile(profile: ProcessingState["profile"]): void;
	setSoftwareVolume(value: number): void;
	enableReplayGain(mode: ReplayGainPreference): void;
	disableReplayGain(): void;
	applyEqualizerPreset(preset: Exclude<EqualizerPreset, "custom">): void;
	setEqualizerGain(index: number, value: number): void;
};

export function PlaybackSignal({
	telemetry,
	outputMode,
	processingControls,
	replayGainMetadata,
}: PlaybackSignalProps) {
	const [isOpen, setIsOpen] = useState(false);
	const processingState = processingControls?.state ?? null;
	const effectiveTelemetry = mergeProcessingState(telemetry, processingState);
	const status = derivePlaybackTelemetryStatus(effectiveTelemetry);
	const statusLabel = formatTelemetryStatus(status);

	return (
		<>
			<button
				type="button"
				aria-label={`Playback signal: ${statusLabel}`}
				className="inline-flex h-6 items-center gap-1.5 rounded-xl border border-[var(--sidebar-border)] bg-[var(--player-pill)] px-2.5 text-[11px] text-player-foreground hover:text-[var(--player-control-primary)]"
				onClick={() => setIsOpen(true)}
			>
				<Activity className="size-3" />
				{statusLabel}
			</button>
			{isOpen ? (
				<PlaybackSignalDialog
					telemetry={effectiveTelemetry}
					outputMode={outputMode}
					processingControls={processingControls}
					processingState={processingState}
					replayGainMetadata={replayGainMetadata}
					onClose={() => setIsOpen(false)}
				/>
			) : null}
		</>
	);
}

function PlaybackSignalDialog({
	telemetry,
	outputMode,
	processingControls,
	processingState,
	replayGainMetadata,
	onClose,
}: PlaybackSignalProps & {
	processingState: ProcessingState | null;
	onClose: () => void;
}) {
	const descriptions = describePlaybackTelemetry(telemetry);
	return (
		<div
			role="dialog"
			aria-modal="true"
			aria-label="Playback signal path"
			className="fixed inset-0 z-50 flex items-center justify-center bg-black/55 p-4"
		>
			<div className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-xl border border-border bg-popover p-5 text-popover-foreground shadow-2xl">
				<SignalDialogHeader onClose={onClose} />
				<SignalPathLayers descriptions={descriptions} />
				{outputMode ? <OutputModeDisplay outputMode={outputMode} /> : null}
				{processingControls && processingState ? (
					<ProcessingControls
						controller={processingControls}
						state={processingState}
						replayGainMetadata={replayGainMetadata}
					/>
				) : null}
			</div>
		</div>
	);
}

function OutputModeDisplay({ outputMode }: { outputMode: OutputMode }) {
	return (
		<p className="mt-3 text-muted-foreground text-xs">
			Output Mode: {outputMode === "system" ? "System Output" : "Unknown"}
		</p>
	);
}

function SignalDialogHeader({ onClose }: { onClose: () => void }) {
	return (
		<header className="mb-4 flex items-start justify-between gap-4">
			<div>
				<h2 className="font-semibold text-base">Playback signal path</h2>
				<p className="text-muted-foreground text-xs">
					Observed values stay separate from unavailable information.
				</p>
			</div>
			<button type="button" aria-label="Close signal path" onClick={onClose}>
				<X className="size-4" />
			</button>
		</header>
	);
}

function SignalPathLayers({
	descriptions,
}: {
	descriptions: PlaybackTelemetryDescriptions;
}) {
	return (
		<div className="grid gap-2 sm:grid-cols-5">
			{Object.entries(descriptions).map(([title, value]) => (
				<SignalLayer key={title} title={capitalize(title)} value={value} />
			))}
		</div>
	);
}

function SignalLayer({ title, value }: { title: string; value: string }) {
	return (
		<section className="min-w-0 rounded-lg border border-border bg-card/45 p-3">
			<h3 className="font-medium text-xs">{title}</h3>
			<p className="mt-1 break-words text-muted-foreground text-[11px]">
				{value}
			</p>
		</section>
	);
}

function ProcessingControls({
	controller,
	state,
	replayGainMetadata,
}: {
	controller: PlaybackProcessingControls;
	state: ProcessingState;
	replayGainMetadata?: Track["replayGain"];
}) {
	return (
		<section className="mt-5 border-border border-t pt-4">
			<div className="flex flex-wrap items-center gap-2">
				<ProfileControl controller={controller} state={state} />
				<ReplayGainControl controller={controller} state={state} />
			</div>
			<TransitionNotice notice={state.transitionNotice} />
			<VolumeAndPresetControls controller={controller} state={state} />
			<EqualizerBands controller={controller} state={state} />
			<ReplayGainAvailability metadata={replayGainMetadata} state={state} />
		</section>
	);
}

function ProfileControl({
	controller,
	state,
}: {
	controller: PlaybackProcessingControls;
	state: ProcessingState;
}) {
	const nextProfile = state.profile === "direct" ? "processed" : "direct";
	return (
		<>
			<strong className="text-sm">
				{state.profile === "direct" ? "Direct Profile" : "Processed Profile"}
			</strong>
			<button
				type="button"
				className="rounded-md border border-border px-2 py-1 text-xs"
				onClick={() => controller.setProfile(nextProfile)}
			>
				Use {capitalize(nextProfile)}
			</button>
		</>
	);
}

function ReplayGainControl({
	controller,
	state,
}: {
	controller: PlaybackProcessingControls;
	state: ProcessingState;
}) {
	const [isChoosingMode, setIsChoosingMode] = useState(false);
	const toggle = () => {
		if (state.replayGainMode !== "off") return controller.disableReplayGain();
		if (state.replayGainPreference) {
			return controller.enableReplayGain(state.replayGainPreference);
		}
		setIsChoosingMode(true);
	};
	return (
		<>
			<button
				type="button"
				className="rounded-md border border-border px-2 py-1 text-xs"
				onClick={toggle}
			>
				{state.replayGainMode === "off"
					? "Enable ReplayGain"
					: "Disable ReplayGain"}
			</button>
			{state.replayGainMode !== "off" ? (
				<span className="text-xs">
					ReplayGain {capitalize(state.replayGainMode)}
				</span>
			) : null}
			{isChoosingMode ? (
				<ReplayGainModeChoice
					onChoose={(mode) => {
						controller.enableReplayGain(mode);
						setIsChoosingMode(false);
					}}
				/>
			) : null}
		</>
	);
}

function ReplayGainModeChoice({
	onChoose,
}: {
	onChoose: (mode: ReplayGainPreference) => void;
}) {
	return (
		<div className="mt-3 w-full rounded-lg border border-border p-3">
			<p className="text-sm">Choose ReplayGain mode</p>
			<div className="mt-2 flex gap-2">
				{(["album", "track"] as const).map((mode) => (
					<button
						type="button"
						key={mode}
						className="rounded-md bg-primary px-2 py-1 text-primary-foreground text-xs"
						onClick={() => onChoose(mode)}
					>
						{capitalize(mode)} mode
					</button>
				))}
			</div>
		</div>
	);
}

function TransitionNotice({ notice }: { notice: string | null }) {
	return notice ? (
		<p role="alert" className="mt-3 text-amber-600 text-xs">
			{notice}
		</p>
	) : null;
}

function VolumeAndPresetControls({
	controller,
	state,
}: {
	controller: PlaybackProcessingControls;
	state: ProcessingState;
}) {
	return (
		<div className="mt-4 grid gap-4 sm:grid-cols-2">
			<label className="grid gap-1 text-xs">
				Software volume
				<input
					type="range"
					aria-label="Software volume"
					min="0"
					max="1"
					step="0.01"
					value={state.softwareVolume}
					onChange={(event) =>
						controller.setSoftwareVolume(Number(event.target.value))
					}
				/>
			</label>
			<EqualizerPresetControl controller={controller} state={state} />
		</div>
	);
}

function EqualizerPresetControl({
	controller,
	state,
}: {
	controller: PlaybackProcessingControls;
	state: ProcessingState;
}) {
	return (
		<label className="grid gap-1 text-xs">
			Equalizer preset
			<select
				aria-label="Equalizer preset"
				value={state.equalizer.preset}
				onChange={(event) =>
					controller.applyEqualizerPreset(
						event.target.value as Exclude<EqualizerPreset, "custom">,
					)
				}
			>
				<option value="custom" disabled>
					Custom
				</option>
				<option value="flat">Flat</option>
				<option value="bass-boost">Bass boost</option>
				<option value="vocal">Vocal</option>
				<option value="treble-boost">Treble boost</option>
			</select>
		</label>
	);
}

function EqualizerBands({
	controller,
	state,
}: {
	controller: PlaybackProcessingControls;
	state: ProcessingState;
}) {
	return (
		<>
			<div className="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-5">
				{EQ_FREQUENCIES_HZ.map((frequency, index) => (
					<EqualizerBand
						key={frequency}
						frequency={frequency}
						gain={state.equalizer.gainsDb[index] ?? 0}
						onChange={(value) => controller.setEqualizerGain(index, value)}
					/>
				))}
			</div>
			<p className="mt-2 text-muted-foreground text-xs">
				Effective mpv EQ: {state.effectiveAudioFilters.length} filters
			</p>
		</>
	);
}

function EqualizerBand({
	frequency,
	gain,
	onChange,
}: {
	frequency: number;
	gain: number;
	onChange: (value: number) => void;
}) {
	return (
		<label className="grid gap-1 text-[10px]">
			{frequency} Hz
			<input
				type="range"
				aria-label={`${frequency} Hz gain`}
				min="-12"
				max="12"
				step="0.5"
				value={gain}
				onChange={(event) => onChange(Number(event.target.value))}
			/>
		</label>
	);
}

function ReplayGainAvailability({
	metadata,
	state,
}: {
	metadata?: Track["replayGain"];
	state: ProcessingState;
}) {
	const { isTrackAvailable, isAlbumAvailable, isUsingTrackFallback } =
		deriveReplayGainAvailability(metadata, state.effectiveReplayGainMode);
	return (
		<p className="mt-3 text-muted-foreground text-xs">
			Track ReplayGain metadata:{" "}
			{isTrackAvailable ? "Available" : "Unavailable"}
			{" · "}Album ReplayGain metadata:{" "}
			{isAlbumAvailable ? "Available" : "Unavailable"}
			{isUsingTrackFallback ? " · Using Track fallback" : null}
		</p>
	);
}

function capitalize(value: string) {
	return value.charAt(0).toUpperCase() + value.slice(1);
}

import type { Track } from "@repo/api-client";
import type { ReplayGainMode } from "./processing";

export type AudioFormatObservation = {
	sampleRateHz: number | null;
	bitDepth: number | null;
	channels: number | null;
};

export type PlaybackTelemetry = {
	source: {
		codec: string | null;
		bitrateKbps: number | null;
		format: AudioFormatObservation;
	};
	decoder: {
		pcmFormat: string | null;
		format: AudioFormatObservation;
	};
	system: {
		kind: "pipewire" | "browser-managed" | "bypassed" | "unknown";
		format: AudioFormatObservation;
		isResampling: boolean | null;
	};
	device: {
		name: string | null;
		format: AudioFormatObservation;
		isResampling: boolean | null;
	};
	processing: {
		profile: "direct" | "processed" | "unknown";
		softwareVolume: number | null;
		replayGainMode: ReplayGainMode | "unknown";
		isEqualizerEnabled: boolean | null;
	};
};

export type PlaybackTelemetryStatus =
	| "format-matched"
	| "processed"
	| "resampled"
	| "unknown";

export function derivePlaybackTelemetryStatus(
	telemetry: PlaybackTelemetry,
): PlaybackTelemetryStatus {
	if (hasActiveProcessing(telemetry.processing)) return "processed";
	if (hasResamplingEvidence(telemetry)) return "resampled";
	if (hasFormatMatchEvidence(telemetry)) return "format-matched";
	return "unknown";
}

export function createBrowserPlaybackTelemetry(
	track: Track | null,
	softwareVolume: number,
): PlaybackTelemetry {
	return {
		source: {
			codec: track?.format?.toUpperCase() ?? null,
			bitrateKbps: track?.bitrateKbps ?? null,
			format: {
				sampleRateHz: track?.sampleRateHz ?? null,
				bitDepth: track?.bitDepth ?? null,
				channels: null,
			},
		},
		decoder: { pcmFormat: null, format: unknownFormat() },
		system: {
			kind: "browser-managed",
			format: unknownFormat(),
			isResampling: null,
		},
		device: { name: null, format: unknownFormat(), isResampling: null },
		processing: {
			profile: softwareVolume === 1 ? "unknown" : "processed",
			softwareVolume,
			replayGainMode: "unknown",
			isEqualizerEnabled: null,
		},
	};
}

export function formatTelemetryStatus(status: PlaybackTelemetryStatus) {
	switch (status) {
		case "format-matched":
			return "Format matched";
		case "processed":
			return "Processed";
		case "resampled":
			return "Resampled";
		case "unknown":
			return "Unknown";
	}
}

function hasActiveProcessing(processing: PlaybackTelemetry["processing"]) {
	return (
		processing.profile === "processed" ||
		(processing.softwareVolume !== null && processing.softwareVolume !== 1) ||
		(processing.replayGainMode !== "off" &&
			processing.replayGainMode !== "unknown") ||
		processing.isEqualizerEnabled === true
	);
}

function hasResamplingEvidence(telemetry: PlaybackTelemetry) {
	if (
		telemetry.system.isResampling === true ||
		telemetry.device.isResampling === true
	)
		return true;
	const formats = [
		telemetry.source.format,
		telemetry.decoder.format,
		telemetry.system.format,
		telemetry.device.format,
	];
	return formats.some((format, index) => {
		const next = formats[index + 1];
		return Boolean(
			format.sampleRateHz &&
				next?.sampleRateHz &&
				format.sampleRateHz !== next.sampleRateHz,
		);
	});
}

function hasFormatMatchEvidence(telemetry: PlaybackTelemetry) {
	const { source, decoder, system, device } = telemetry;
	if (!hasRequiredSourceFormat(source.format)) return false;
	if (![decoder.format, system.format, device.format].every(isCompleteFormat))
		return false;
	return (
		knownFormatsMatch(source.format, decoder.format) &&
		formatsMatch(decoder.format, system.format) &&
		formatsMatch(system.format, device.format)
	);
}

function hasRequiredSourceFormat(format: AudioFormatObservation) {
	return format.sampleRateHz !== null && format.bitDepth !== null;
}

function knownFormatsMatch(
	left: AudioFormatObservation,
	right: AudioFormatObservation,
) {
	return (Object.keys(left) as Array<keyof AudioFormatObservation>).every(
		(key) =>
			left[key] === null || right[key] === null || left[key] === right[key],
	);
}

function formatsMatch(
	left: AudioFormatObservation,
	right: AudioFormatObservation,
) {
	return (
		left.sampleRateHz === right.sampleRateHz &&
		left.bitDepth === right.bitDepth &&
		left.channels === right.channels
	);
}

function isCompleteFormat(format: AudioFormatObservation) {
	return (
		format.sampleRateHz !== null &&
		format.bitDepth !== null &&
		format.channels !== null
	);
}

function unknownFormat(): AudioFormatObservation {
	return { sampleRateHz: null, bitDepth: null, channels: null };
}

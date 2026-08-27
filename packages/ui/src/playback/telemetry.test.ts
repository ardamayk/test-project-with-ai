import type { Track } from "@repo/api-client";
import { describe, expect, it } from "vitest";
import {
	createBrowserPlaybackTelemetry,
	derivePlaybackTelemetryStatus,
	type PlaybackTelemetry,
} from "./telemetry";

const MATCHED_TELEMETRY: PlaybackTelemetry = {
	source: {
		codec: "FLAC",
		bitrateKbps: 1411,
		format: { sampleRateHz: 96000, bitDepth: 24, channels: 2 },
	},
	decoder: {
		pcmFormat: "s24",
		format: { sampleRateHz: 96000, bitDepth: 24, channels: 2 },
	},
	system: {
		kind: "pipewire",
		format: { sampleRateHz: 96000, bitDepth: 24, channels: 2 },
		isResampling: false,
	},
	device: {
		name: "USB DAC",
		format: { sampleRateHz: 96000, bitDepth: 24, channels: 2 },
		isResampling: false,
	},
	processing: {
		profile: "direct",
		softwareVolume: 1,
		replayGainMode: "off",
		isEqualizerEnabled: false,
	},
};

describe("derivePlaybackTelemetryStatus", () => {
	it("reports Format matched only when observed formats agree without processing", () => {
		expect(derivePlaybackTelemetryStatus(MATCHED_TELEMETRY)).toBe(
			"format-matched",
		);
	});

	it("matches the real Track contract without inventing unavailable Source channels", () => {
		expect(
			derivePlaybackTelemetryStatus({
				...MATCHED_TELEMETRY,
				source: {
					...MATCHED_TELEMETRY.source,
					format: {
						...MATCHED_TELEMETRY.source.format,
						channels: null,
					},
				},
			}),
		).toBe("format-matched");
	});

	it("reports Processed from explicit active processing evidence", () => {
		expect(
			derivePlaybackTelemetryStatus({
				...MATCHED_TELEMETRY,
				processing: {
					...MATCHED_TELEMETRY.processing,
					profile: "processed",
					replayGainMode: "album",
				},
			}),
		).toBe("processed");
	});

	it("reports Resampled from observed PipeWire negotiation", () => {
		expect(
			derivePlaybackTelemetryStatus({
				...MATCHED_TELEMETRY,
				system: {
					kind: "pipewire",
					format: { sampleRateHz: 48000, bitDepth: 24, channels: 2 },
					isResampling: true,
				},
			}),
		).toBe("resampled");
	});

	it("reports Unknown when the system path is only partially observed", () => {
		expect(
			derivePlaybackTelemetryStatus({
				...MATCHED_TELEMETRY,
				system: {
					kind: "pipewire",
					format: {
						sampleRateHz: null,
						bitDepth: null,
						channels: null,
					},
					isResampling: null,
				},
				device: {
					name: null,
					format: {
						sampleRateHz: null,
						bitDepth: null,
						channels: null,
					},
					isResampling: null,
				},
			}),
		).toBe("unknown");
	});

	it("does not report an end-to-end match without observed Device format", () => {
		expect(
			derivePlaybackTelemetryStatus({
				...MATCHED_TELEMETRY,
				device: {
					name: null,
					format: unknownFormat(),
					isResampling: null,
				},
			}),
		).toBe("unknown");
	});

	it("reports Unknown when no layer contains usable evidence", () => {
		expect(
			derivePlaybackTelemetryStatus({
				source: {
					codec: null,
					bitrateKbps: null,
					format: unknownFormat(),
				},
				decoder: { pcmFormat: null, format: unknownFormat() },
				system: {
					kind: "unknown",
					format: unknownFormat(),
					isResampling: null,
				},
				device: {
					name: null,
					format: unknownFormat(),
					isResampling: null,
				},
				processing: {
					profile: "unknown",
					softwareVolume: null,
					replayGainMode: "unknown",
					isEqualizerEnabled: null,
				},
			}),
		).toBe("unknown");
	});
});

describe("createBrowserPlaybackTelemetry", () => {
	it("uses source metadata without inventing decoder or DAC observations", () => {
		const track = {
			format: "flac",
			bitrateKbps: 1411,
			sampleRateHz: 96000,
			bitDepth: 24,
		} as Track;

		const telemetry = createBrowserPlaybackTelemetry(track, 1);

		expect(telemetry.source).toMatchObject({
			codec: "FLAC",
			bitrateKbps: 1411,
			format: { sampleRateHz: 96000, bitDepth: 24 },
		});
		expect(telemetry.decoder.format).toEqual(unknownFormat());
		expect(telemetry.system.kind).toBe("browser-managed");
		expect(telemetry.device.name).toBeNull();
		expect(derivePlaybackTelemetryStatus(telemetry)).toBe("unknown");
	});
});

function unknownFormat() {
	return { sampleRateHz: null, bitDepth: null, channels: null };
}

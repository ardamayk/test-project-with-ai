import {
	cleanup,
	fireEvent,
	render,
	screen,
	within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ProcessingState } from "../playback/processing";
import {
	createBrowserPlaybackTelemetry,
	type PlaybackTelemetry,
} from "../playback/telemetry";
import {
	type PlaybackProcessingControls,
	PlaybackSignal,
} from "./PlaybackSignal";

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
		effectiveReplayGainMode: "off",
		isEqualizerEnabled: false,
	},
};

const PROCESSED_STATE: ProcessingState = {
	profile: "processed",
	softwareVolume: 0.5,
	replayGainMode: "off",
	effectiveReplayGainMode: "off",
	replayGainPreference: null,
	equalizer: {
		isEnabled: false,
		preset: "flat",
		gainsDb: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
	},
	effectiveAudioFilters: [],
	transitionNotice: "Software volume requires the Processed Profile.",
};

function createControls(
	state: ProcessingState = PROCESSED_STATE,
): PlaybackProcessingControls {
	return {
		state,
		setProfile: vi.fn(),
		setSoftwareVolume: vi.fn(),
		enableReplayGain: vi.fn(),
		disableReplayGain: vi.fn(),
		applyEqualizerPreset: vi.fn(),
		setEqualizerGain: vi.fn(),
	};
}

describe("PlaybackSignal", () => {
	afterEach(cleanup);

	it("opens an evidence-only Source to Processing detail view", () => {
		render(<PlaybackSignal telemetry={MATCHED_TELEMETRY} />);

		fireEvent.click(
			screen.getByRole("button", { name: "Playback signal: Format matched" }),
		);

		const dialog = screen.getByRole("dialog", { name: "Playback signal path" });
		for (const layer of [
			"Source",
			"Decoder",
			"System",
			"Device",
			"Processing",
		]) {
			expect(within(dialog).getByRole("heading", { name: layer })).toBeTruthy();
		}
		expect(within(dialog).getByText(/USB DAC/)).toBeTruthy();
		expect(within(dialog).queryByText(/bit-perfect/i)).toBeNull();
	});

	it("labels unavailable browser observations without copying source values", () => {
		const telemetry = createBrowserPlaybackTelemetry(
			{
				format: "flac",
				bitrateKbps: 1411,
				sampleRateHz: 96000,
				bitDepth: 24,
			} as never,
			1,
		);
		render(<PlaybackSignal telemetry={telemetry} />);

		fireEvent.click(
			screen.getByRole("button", { name: "Playback signal: Unknown" }),
		);
		const dialog = screen.getByRole("dialog", { name: "Playback signal path" });
		const system = within(dialog).getByRole("heading", {
			name: "System",
		}).parentElement;
		const device = within(dialog).getByRole("heading", {
			name: "Device",
		}).parentElement;

		expect(system?.textContent).toContain("Browser managed");
		expect(device?.textContent).toContain("Unknown");
		expect(device?.textContent).not.toContain("96 kHz");
	});

	it("shows and explains processing changes through observable controls", () => {
		const controls = createControls();
		render(
			<PlaybackSignal
				telemetry={MATCHED_TELEMETRY}
				outputMode="system"
				processingControls={controls}
			/>,
		);
		fireEvent.click(
			screen.getByRole("button", { name: "Playback signal: Processed" }),
		);

		fireEvent.change(screen.getByLabelText("Software volume"), {
			target: { value: "0.4" },
		});

		expect(screen.getByText("Processed Profile")).toBeTruthy();
		expect(screen.getByText("Output Mode: System Output")).toBeTruthy();
		expect(screen.getByRole("alert").textContent).toContain(
			"Software volume requires the Processed Profile",
		);

		fireEvent.click(screen.getByRole("button", { name: "Enable ReplayGain" }));
		expect(screen.getByText("Choose ReplayGain mode")).toBeTruthy();
		fireEvent.click(screen.getByRole("button", { name: "Album mode" }));
		expect(controls.enableReplayGain).toHaveBeenCalledWith("album");

		fireEvent.change(screen.getByLabelText("Equalizer preset"), {
			target: { value: "vocal" },
		});
		expect(controls.setSoftwareVolume).toHaveBeenCalledWith(0.4);
		expect(controls.applyEqualizerPreset).toHaveBeenCalledWith("vocal");
		expect(screen.getAllByRole("slider", { name: /Hz gain/ })).toHaveLength(10);
		expect(screen.getByText("Output Mode: System Output")).toBeTruthy();
	});

	it("labels active custom equalizer gains as Custom", () => {
		const controls = createControls({
			...PROCESSED_STATE,
			effectiveAudioFilters: ["equalizer=f=31.25:t=q:w=1:g=3"],
			equalizer: {
				isEnabled: true,
				preset: "custom",
				gainsDb: [3, 0, 0, 0, 0, 0, 0, 0, 0, 0],
			},
		});
		render(
			<PlaybackSignal
				telemetry={MATCHED_TELEMETRY}
				processingControls={controls}
			/>,
		);

		fireEvent.click(
			screen.getByRole("button", { name: "Playback signal: Processed" }),
		);

		expect(
			(
				screen.getByRole("combobox", {
					name: "Equalizer preset",
				}) as HTMLSelectElement
			).value,
		).toBe("custom");
		expect(screen.getByText("Effective mpv EQ: 1 filters")).toBeTruthy();
	});

	it("shows Album metadata availability and observed Track fallback", () => {
		const controls = createControls({
			...PROCESSED_STATE,
			replayGainMode: "album",
			replayGainPreference: "album",
			effectiveReplayGainMode: "track-fallback",
		});
		render(
			<PlaybackSignal
				telemetry={MATCHED_TELEMETRY}
				processingControls={controls}
				replayGainMetadata={{
					trackGainDb: -7.25,
					trackPeak: 0.9,
					albumGainDb: null,
					albumPeak: null,
				}}
			/>,
		);

		fireEvent.click(
			screen.getByRole("button", { name: "Playback signal: Processed" }),
		);

		expect(screen.getByText(/Using Track fallback/)).toBeTruthy();
		expect(screen.getByText(/Effective Track fallback/)).toBeTruthy();
	});
});

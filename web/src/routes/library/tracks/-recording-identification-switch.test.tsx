import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ getHealth: vi.fn() }));

vi.mock("#/lib/api", () => ({
	apiClient: { getHealth: mocks.getHealth },
}));

import {
	RecordingIdentificationSwitch,
	readStoredRecordingIdentification,
	recordingIdentificationAvailability,
} from "./-recording-identification-switch";

function health(status: string, keySource = "embedded") {
	return {
		status: "ok",
		version: "0.1.0",
		capabilities: ["api.v1"],
		dependencies: [],
		recordingIdentification: { status, acoustIdKeySource: keySource },
	};
}

function renderSwitch(onChange: (isOn: boolean) => void, isDisabled = false) {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return render(
		<QueryClientProvider client={client}>
			<RecordingIdentificationSwitch
				isDisabled={isDisabled}
				onChange={onChange}
			/>
		</QueryClientProvider>,
	);
}

describe("recordingIdentificationAvailability", () => {
	it("is available only when the server reports enabled", () => {
		expect(
			recordingIdentificationAvailability(health("enabled") as never),
		).toEqual({ isAvailable: true });
		expect(
			recordingIdentificationAvailability(health("missing_fpcalc") as never)
				.reason,
		).toContain("fpcalc");
		expect(recordingIdentificationAvailability(undefined).isAvailable).toBe(
			false,
		);
	});

	it("treats a server without the field as unsupported", () => {
		const legacy = { status: "ok", version: "0.0.9", capabilities: [] };
		expect(
			recordingIdentificationAvailability(legacy as never).reason,
		).toContain("not supported");
	});
});

describe("RecordingIdentificationSwitch", () => {
	afterEach(() => {
		cleanup();
		try {
			localStorage.clear();
		} catch {}
	});

	it("is on by default when the server can identify recordings", async () => {
		mocks.getHealth.mockResolvedValue(health("enabled"));
		const onChange = vi.fn();
		renderSwitch(onChange);

		await waitFor(() => expect(onChange).toHaveBeenLastCalledWith(true));
		const control = screen.getByRole("switch", {
			name: "Identify recordings with MusicBrainz",
		});
		expect(control.getAttribute("aria-checked")).toBe("true");
		expect(control.hasAttribute("disabled")).toBe(false);
	});

	it("stays off and disabled with the server's reason when unavailable", async () => {
		mocks.getHealth.mockResolvedValue(health("missing_fpcalc", "missing"));
		const onChange = vi.fn();
		renderSwitch(onChange);

		await waitFor(() =>
			expect(screen.getByText(/fpcalc is not installed/)).toBeTruthy(),
		);
		const control = screen.getByRole("switch", {
			name: "Identify recordings with MusicBrainz",
		});
		expect(control.getAttribute("aria-checked")).toBe("false");
		expect(control.hasAttribute("disabled")).toBe(true);
		expect(onChange).toHaveBeenLastCalledWith(false);
	});

	it("remembers the last choice in this browser", async () => {
		mocks.getHealth.mockResolvedValue(health("enabled"));
		const onChange = vi.fn();
		renderSwitch(onChange);
		const control = await screen.findByRole("switch", {
			name: "Identify recordings with MusicBrainz",
		});
		await waitFor(() =>
			expect(control.getAttribute("aria-checked")).toBe("true"),
		);

		fireEvent.click(control);

		await waitFor(() =>
			expect(control.getAttribute("aria-checked")).toBe("false"),
		);
		expect(onChange).toHaveBeenLastCalledWith(false);
		expect(readStoredRecordingIdentification()).toBe(false);
	});
});

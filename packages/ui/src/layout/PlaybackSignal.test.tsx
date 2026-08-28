import {
	cleanup,
	fireEvent,
	render,
	screen,
	within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { type PlaybackOutputControls, PlaybackSignal } from "./PlaybackSignal";

function createControls(): PlaybackOutputControls {
	return {
		selectNormalOutput: vi.fn(),
		selectExclusiveOutput: vi.fn(),
		enableAdaptiveSystemRate: vi.fn(),
	};
}

describe("PlaybackSignal", () => {
	afterEach(cleanup);

	it("opens a compact three-option menu above the active output label", () => {
		const controls = createControls();
		render(<PlaybackSignal outputMode="system" outputControls={controls} />);

		const trigger = screen.getByRole("button", { name: "Output mode: Normal" });
		fireEvent.click(trigger);

		const menu = screen.getByRole("menu", { name: "Output mode" });
		expect(within(menu).getAllByRole("menuitemradio")).toHaveLength(3);
		expect(
			within(menu)
				.getByRole("menuitemradio", { name: "Normal" })
				.getAttribute("aria-checked"),
		).toBe("true");
		expect(
			within(menu).getByRole("menuitemradio", { name: "Exclusive" }),
		).toBeTruthy();
		expect(
			within(menu).getByRole("menuitemradio", { name: "Adaptive" }),
		).toBeTruthy();
	});

	it("selects Normal and Exclusive without asking for a device", () => {
		const controls = createControls();
		const { rerender } = render(
			<PlaybackSignal outputMode="direct-alsa" outputControls={controls} />,
		);

		fireEvent.click(
			screen.getByRole("button", { name: "Output mode: Exclusive" }),
		);
		fireEvent.click(screen.getByRole("menuitemradio", { name: "Normal" }));
		expect(controls.selectNormalOutput).toHaveBeenCalledOnce();
		expect(screen.queryByRole("menu", { name: "Output mode" })).toBeNull();

		rerender(<PlaybackSignal outputMode="system" outputControls={controls} />);
		fireEvent.click(
			screen.getByRole("button", { name: "Output mode: Normal" }),
		);
		fireEvent.click(screen.getByRole("menuitemradio", { name: "Exclusive" }));
		expect(controls.selectExclusiveOutput).toHaveBeenCalledOnce();
		expect(screen.queryByText(/USB|HDMI|hw:/i)).toBeNull();
	});

	it("selects Adaptive once and closes the compact menu", () => {
		const controls = createControls();
		render(<PlaybackSignal outputMode="system" outputControls={controls} />);

		fireEvent.click(
			screen.getByRole("button", { name: "Output mode: Normal" }),
		);
		fireEvent.click(screen.getByRole("menuitemradio", { name: "Adaptive" }));

		expect(controls.enableAdaptiveSystemRate).toHaveBeenCalledOnce();
		expect(controls.enableAdaptiveSystemRate).toHaveBeenCalledWith();
		expect(screen.queryByRole("menu", { name: "Output mode" })).toBeNull();
		expect(screen.queryByRole("alert")).toBeNull();
	});

	it("closes on Escape and outside pointer input", () => {
		const controls = createControls();
		render(
			<div>
				<PlaybackSignal
					outputMode="adaptive-system-rate"
					outputControls={controls}
				/>
				<button type="button">Outside</button>
			</div>,
		);

		fireEvent.click(
			screen.getByRole("button", { name: "Output mode: Adaptive" }),
		);
		fireEvent.keyDown(document, { key: "Escape" });
		expect(screen.queryByRole("menu", { name: "Output mode" })).toBeNull();

		fireEvent.click(
			screen.getByRole("button", { name: "Output mode: Adaptive" }),
		);
		fireEvent.mouseDown(screen.getByRole("button", { name: "Outside" }));
		expect(screen.queryByRole("menu", { name: "Output mode" })).toBeNull();
	});
});

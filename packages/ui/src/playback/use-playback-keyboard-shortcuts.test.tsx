import { cleanup, fireEvent, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { usePlaybackKeyboardShortcuts } from "./use-playback-keyboard-shortcuts";

function Harness({
	togglePlay,
	navigatePrevious,
	navigateNext,
}: {
	togglePlay: () => void;
	navigatePrevious: () => void;
	navigateNext: () => void;
}) {
	usePlaybackKeyboardShortcuts({ togglePlay, navigatePrevious, navigateNext });
	return (
		<div>
			<input aria-label="Search" type="text" />
			<button type="button">Play</button>
			<p>Body</p>
		</div>
	);
}

function renderHarness() {
	const actions = {
		togglePlay: vi.fn(),
		navigatePrevious: vi.fn(),
		navigateNext: vi.fn(),
	};
	const result = render(<Harness {...actions} />);
	return { ...result, ...actions };
}

describe("usePlaybackKeyboardShortcuts", () => {
	afterEach(cleanup);

	it("toggles playback on Space when nothing editable is focused", () => {
		const { togglePlay } = renderHarness();

		const event = new KeyboardEvent("keydown", {
			key: " ",
			bubbles: true,
			cancelable: true,
		});
		document.body.dispatchEvent(event);

		expect(togglePlay).toHaveBeenCalledTimes(1);
		expect(event.defaultPrevented).toBe(true);
	});

	it("leaves Space alone inside text inputs and on buttons", () => {
		const { togglePlay, getByLabelText, getByRole } = renderHarness();

		fireEvent.keyDown(getByLabelText("Search"), { key: " " });
		fireEvent.keyDown(getByRole("button", { name: "Play" }), { key: " " });

		expect(togglePlay).not.toHaveBeenCalled();
	});

	it("ignores held-down key repeats and modified Space", () => {
		const { togglePlay } = renderHarness();

		fireEvent.keyDown(document.body, { key: " ", repeat: true });
		fireEvent.keyDown(document.body, { key: " ", ctrlKey: true });

		expect(togglePlay).not.toHaveBeenCalled();
	});

	it("maps media keys to playback actions", () => {
		const { togglePlay, navigateNext, navigatePrevious } = renderHarness();

		fireEvent.keyDown(document.body, { key: "MediaPlayPause" });
		fireEvent.keyDown(document.body, { key: "MediaTrackNext" });
		fireEvent.keyDown(document.body, { key: "MediaTrackPrevious" });

		expect(togglePlay).toHaveBeenCalledTimes(1);
		expect(navigateNext).toHaveBeenCalledTimes(1);
		expect(navigatePrevious).toHaveBeenCalledTimes(1);
	});

	it("stops listening after unmount", () => {
		const { togglePlay, unmount } = renderHarness();
		unmount();

		fireEvent.keyDown(document.body, { key: " " });

		expect(togglePlay).not.toHaveBeenCalled();
	});
});

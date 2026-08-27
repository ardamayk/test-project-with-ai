import { afterEach, describe, expect, it, vi } from "vitest";
import { createPlaybackEngine } from "./create-playback-engine";
import { DesktopSourceRoutingPlaybackEngine } from "./DesktopSourceRoutingPlaybackEngine";

describe("createPlaybackEngine", () => {
	afterEach(() => {
		Reflect.deleteProperty(window, "__TAURI_INTERNALS__");
		vi.unstubAllGlobals();
	});

	it("uses native playback in Desktop without creating HTML audio", () => {
		Object.defineProperty(window, "__TAURI_INTERNALS__", {
			configurable: true,
			value: {},
		});
		const audioConstructor = vi.fn();
		vi.stubGlobal("Audio", audioConstructor);

		const engine = createPlaybackEngine();

		expect(engine).toBeInstanceOf(DesktopSourceRoutingPlaybackEngine);
		expect(audioConstructor).not.toHaveBeenCalled();
		engine.destroy();
	});
});

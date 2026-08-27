import type {
	PlaybackSessionListener,
	PlaybackSessionState,
	PlaybackSource,
} from "@repo/ui";
import { DEFAULT_PLAYBACK_SESSION_STATE } from "@repo/ui";
import { describe, expect, it, vi } from "vitest";
import { DesktopPlaybackEngine } from "./DesktopPlaybackEngine";

const trackSource: PlaybackSource = {
	type: "track",
	track: {
		id: "track-1",
		title: "Track 1",
		artistName: "Artist",
		albumId: "album-1",
		durationMs: 120000,
		format: "flac",
	},
	playbackUrl: `http://127.0.0.1:43129/${"a".repeat(64)}/api/v1/tracks/track-1/stream`,
};

function createBridge() {
	let listener: PlaybackSessionListener | null = null;
	let state: PlaybackSessionState = { ...DEFAULT_PLAYBACK_SESSION_STATE };
	return {
		bridge: {
			getState: vi.fn(async () => state),
			play: vi.fn(async (source?: PlaybackSource) => {
				state = {
					...state,
					source: source ?? state.source,
					status: "playing",
					duration: 120,
				};
				return state;
			}),
			pause: vi.fn(async () => state),
			stop: vi.fn(async () => state),
			togglePlay: vi.fn(async () => state),
			seek: vi.fn(async () => state),
			setVolume: vi.fn(async () => state),
			toggleShuffle: vi.fn(async () => state),
			cycleRepeatMode: vi.fn(async () => state),
			listen: vi.fn(async (nextListener: PlaybackSessionListener) => {
				listener = nextListener;
				return () => {
					listener = null;
				};
			}),
		},
		emit(nextState: PlaybackSessionState) {
			listener?.(nextState);
		},
	};
}

describe("DesktopPlaybackEngine", () => {
	it("plays a Track and projects native timing events", async () => {
		const native = createBridge();
		const engine = new DesktopPlaybackEngine(native.bridge);
		const observedStates: PlaybackSessionState[] = [];
		engine.subscribe((state) => observedStates.push(state));

		await engine.play(trackSource);
		native.emit({
			...engine.getState(),
			currentTime: 15.5,
			duration: 121.25,
		});

		expect(native.bridge.play).toHaveBeenCalledWith(trackSource);
		expect(engine.getState()).toMatchObject({
			source: trackSource,
			status: "playing",
			currentTime: 15.5,
			duration: 121.25,
		});
		expect(observedStates.at(-1)?.currentTime).toBe(15.5);
		engine.destroy();
	});

	it("preserves actionable native playback errors", async () => {
		const native = createBridge();
		native.bridge.play.mockRejectedValue({
			code: "playback-failed",
			message: "Pinned mpv could not decode this Track.",
		});
		const engine = new DesktopPlaybackEngine(native.bridge);

		await expect(engine.play(trackSource)).rejects.toMatchObject({
			code: "playback-failed",
		});

		expect(engine.getState()).toMatchObject({
			status: "error",
			error: {
				code: "playback-failed",
				message: "Pinned mpv could not decode this Track.",
			},
		});
		engine.destroy();
	});
});

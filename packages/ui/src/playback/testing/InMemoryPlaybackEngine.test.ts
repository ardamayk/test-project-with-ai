import { InMemoryPlaybackEngine } from "./InMemoryPlaybackEngine";
import { playbackEngineContract } from "./playback-engine-contract";

playbackEngineContract("in-memory", () => {
	const engine = new InMemoryPlaybackEngine();
	return {
		engine,
		finish: () => engine.finish(),
	};
});

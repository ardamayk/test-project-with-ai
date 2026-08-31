import { describe, expect, it } from "vitest";
import { formatTrackMeta } from "./format-track-meta";

describe("formatTrackMeta", () => {
	it("joins genre, format, bit depth, and sample rate", () => {
		const meta = formatTrackMeta({
			id: "1",
			title: "Track",
			artistName: "Artist",
			artists: [],
			albumId: "a1",
			discNo: 1,
			durationMs: 1000,
			format: "flac",
			genre: "Pop",
			genres: [{ id: "genre-pop", name: "Pop" }],
			bitDepth: 24,
			sampleRateHz: 96000,
		});
		expect(meta).toBe("Pop · FLAC · 24-bit · 96 kHz");
	});

	it("falls back to bitrate when sample rate is missing", () => {
		const meta = formatTrackMeta({
			id: "1",
			title: "Track",
			artistName: "Artist",
			artists: [],
			albumId: "a1",
			discNo: 1,
			durationMs: 1000,
			format: "mp3",
			genres: [],
			bitrateKbps: 320,
		});
		expect(meta).toBe("MP3 · 320 kbps");
	});
});

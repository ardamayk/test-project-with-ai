import { describe, expect, it } from "vitest";
import {
	type ImportFileEntry,
	summarizeImportEntries,
} from "./-managed-import-workflow";

function entry(patch: Partial<ImportFileEntry>): ImportFileEntry {
	return {
		key: crypto.randomUUID(),
		file: new File(["x"], "x.flac"),
		progress: 0,
		state: "unresolved",
		selected: false,
		hasSelectionOverride: false,
		...patch,
	};
}

describe("summarizeImportEntries", () => {
	it("never reports an unresolved row as finished", () => {
		const summary = summarizeImportEntries([
			entry({ progress: 100 }),
			entry({ progress: 40 }),
		]);
		expect(summary.unresolved).toBe(2);
		expect(summary.processed).toBe(0);
		expect(summary.percent).toBe(70);
	});

	it("counts only undecided Possible Duplicates as needing review", () => {
		const possibleDuplicate = {
			duplicateClassification: "possible_duplicate",
		} as NonNullable<ImportFileEntry["preview"]>;
		const summary = summarizeImportEntries([
			entry({ state: "accepted", progress: 100, preview: possibleDuplicate }),
			entry({
				state: "accepted",
				progress: 100,
				preview: possibleDuplicate,
				duplicateDecision: "import_separately",
			}),
			entry({ state: "rejected", progress: 100 }),
			entry({ state: "completed", progress: 100, outcome: "imported" }),
		]);
		expect(summary).toMatchObject({
			total: 4,
			processed: 4,
			needsReview: 1,
			accepted: 1,
			rejected: 1,
			completed: 1,
			percent: 100,
		});
	});

	it("is empty-safe", () => {
		expect(summarizeImportEntries([]).percent).toBe(0);
	});
});

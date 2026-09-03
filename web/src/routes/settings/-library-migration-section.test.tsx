import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LibraryMigrationSection } from "./-library-migration-section";

const mocks = vi.hoisted(() => ({
	getHealth: vi.fn(),
	listTracks: vi.fn(),
	previewLibraryMigration: vi.fn(),
	stageLibraryMigration: vi.fn(),
	cutoverLibraryMigration: vi.fn(),
	previewLibraryMigrationCleanup: vi.fn(),
	cleanupLibraryMigrationSources: vi.fn(),
}));

vi.mock("#/lib/api", () => ({
	apiClient: {
		getHealth: mocks.getHealth,
		listTracks: mocks.listTracks,
		previewLibraryMigration: mocks.previewLibraryMigration,
		stageLibraryMigration: mocks.stageLibraryMigration,
		cutoverLibraryMigration: mocks.cutoverLibraryMigration,
		previewLibraryMigrationCleanup: mocks.previewLibraryMigrationCleanup,
		cleanupLibraryMigrationSources: mocks.cleanupLibraryMigrationSources,
	},
}));

const LEGACY_ID = "11111111-1111-4111-8111-111111111111";
const REJECTED_ID = "22222222-2222-4222-8222-222222222222";
const MIGRATED_ID = "33333333-3333-4333-8333-333333333333";
const TWO_MIB = 2 * 1024 * 1024;

const health = {
	status: "ok",
	version: "test",
	capabilities: ["managed-import.v1", "library-migration.v1"],
};

const rejectedFile = {
	trackId: REJECTED_ID,
	originalFilename: "no-cover.mp3",
	state: "rejected",
	errorCode: "missing_artwork",
	errorField: "artwork",
	errorReason:
		"Embedded front-cover artwork is required; add one with MusicBrainz Picard and retry",
};

const preview = {
	acceptedCount: 1,
	rejectedCount: 1,
	files: [
		{
			trackId: LEGACY_ID,
			originalFilename: "legacy.mp3",
			state: "accepted",
			preview: {
				format: "mp3",
				originalFilename: "legacy.mp3",
				title: "Legacy Song",
				artists: ["Artist"],
				albumArtists: ["Artist"],
				album: "Legacy Album",
				genres: ["Rock"],
				trackNo: 1,
				discNo: 1,
				durationMs: 1000,
				sampleRateHz: 44100,
				channelCount: 2,
				bitrateKbps: 128,
				artworkSha256: "a".repeat(64),
				artworkMediaType: "image/png",
			},
		},
		rejectedFile,
	],
};

const stage = {
	verifiedCount: 1,
	rejectedCount: 1,
	failedCount: 0,
	files: [
		{
			trackId: LEGACY_ID,
			originalFilename: "legacy.mp3",
			state: "verified",
			pendingTrackId: MIGRATED_ID,
			sourceSha256: "b".repeat(64),
			pendingSha256: "b".repeat(64),
		},
		rejectedFile,
	],
};

const cutover = {
	migratedCount: 1,
	rejectedCount: 1,
	failedCount: 0,
	notAttemptedCount: 0,
	files: [
		{
			trackId: LEGACY_ID,
			originalFilename: "legacy.mp3",
			state: "migrated",
			createdTrackId: MIGRATED_ID,
			contentSha256: "b".repeat(64),
		},
		rejectedFile,
	],
};

const ineligibleFile = {
	trackId: REJECTED_ID,
	originalFilename: "no-cover.mp3",
	state: "ineligible",
	errorCode: "legacy_source_not_migrated",
	errorReason: "Legacy source has not been migrated",
};

const cleanupPreview = {
	eligibleCount: 1,
	ineligibleCount: 1,
	totalSizeBytes: TWO_MIB,
	files: [
		{
			trackId: MIGRATED_ID,
			sourceTrackId: LEGACY_ID,
			originalFilename: "legacy.mp3",
			state: "eligible",
			sizeBytes: TWO_MIB,
			contentSha256: "b".repeat(64),
		},
		ineligibleFile,
	],
};

const CUTOVER_DIALOG = "Activate migrated Tracks?";
const CLEANUP_DIALOG = "Permanently delete legacy source files?";

function renderSection() {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	const invalidate = vi.spyOn(queryClient, "invalidateQueries");
	render(
		<QueryClientProvider client={queryClient}>
			<LibraryMigrationSection />
		</QueryClientProvider>,
	);
	return { invalidate };
}

function button(name: string, container: HTMLElement = document.body) {
	return within(container).getByRole("button", { name });
}

function isDisabled(element: HTMLElement) {
	return element.hasAttribute("disabled");
}

async function analyze() {
	fireEvent.click(
		await screen.findByRole("button", { name: "Analyze library" }),
	);
	await screen.findByText("Accepted");
}

async function stageAndOpenCutover() {
	await analyze();
	fireEvent.click(button("Copy and verify"));
	await screen.findByRole("table", { name: /Copies: 1 verified/ });
	fireEvent.click(button("Activate migrated Tracks…"));
	return screen.findByRole("dialog", { name: CUTOVER_DIALOG });
}

async function openCleanup() {
	fireEvent.click(
		await screen.findByRole("button", { name: "Clean up legacy sources…" }),
	);
	return screen.findByRole("dialog", { name: CLEANUP_DIALOG });
}

describe("LibraryMigrationSection", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mocks.getHealth.mockResolvedValue(health);
		mocks.listTracks.mockResolvedValue({
			total: 2,
			items: [
				{ id: LEGACY_ID, title: "Legacy Song", sourceKind: "legacy" },
				{ id: "managed", title: "Managed Song", sourceKind: "managed" },
			],
		});
		mocks.previewLibraryMigration.mockResolvedValue(preview);
		mocks.stageLibraryMigration.mockResolvedValue(stage);
		mocks.cutoverLibraryMigration.mockResolvedValue(cutover);
		mocks.previewLibraryMigrationCleanup.mockResolvedValue(cleanupPreview);
		mocks.cleanupLibraryMigrationSources.mockResolvedValue({
			deletedCount: 1,
			failedCount: 0,
			deletedBytes: TWO_MIB,
			prunedDirectoryCount: 1,
			files: [
				{
					trackId: MIGRATED_ID,
					sourceTrackId: LEGACY_ID,
					originalFilename: "legacy.mp3",
					state: "deleted",
					sizeBytes: TWO_MIB,
				},
			],
		});
	});
	afterEach(cleanup);

	it("shows the Legacy Track count and gates later steps until analysis", async () => {
		renderSection();
		const count = await screen.findByTestId("legacy-track-count");
		expect(count.textContent).toContain("1 Legacy Track");
		expect(isDisabled(button("Copy and verify"))).toBe(true);
		expect(isDisabled(button("Activate migrated Tracks…"))).toBe(true);
		expect(isDisabled(button("Analyze library"))).toBe(false);
	});

	it("marks the count as a lower bound when the library exceeds the sampled page", async () => {
		mocks.listTracks.mockResolvedValue({
			total: 500,
			items: [{ id: LEGACY_ID, title: "Legacy Song", sourceKind: "legacy" }],
		});
		renderSection();
		const count = await screen.findByTestId("legacy-track-count");
		expect(count.textContent).toBe("At least 1 Legacy Track");
	});

	it("disables migration when the Music Server lacks the capability", async () => {
		mocks.getHealth.mockResolvedValue({ ...health, capabilities: [] });
		renderSection();
		await screen.findByTestId("legacy-track-count");
		await waitFor(() =>
			expect(isDisabled(button("Analyze library"))).toBe(true),
		);
		expect(isDisabled(button("Clean up legacy sources…"))).toBe(true);
	});

	it("renders accepted and rejected files with their structured reasons", async () => {
		renderSection();
		await analyze();
		const table = screen.getByRole("table", { name: /Analysis: 1 accepted/ });
		within(table).getByText("legacy.mp3");
		within(table).getByText("Artist · Legacy Album");
		within(table).getByText("Rejected");
		within(table).getByText(/add one with MusicBrainz Picard/);
		expect(mocks.previewLibraryMigration).toHaveBeenCalledTimes(1);
		expect(isDisabled(button("Copy and verify"))).toBe(false);
	});

	it("stages copies, requires a cutover confirmation that states the consequence, then activates", async () => {
		const { invalidate } = renderSection();
		const dialog = await stageAndOpenCutover();
		expect(mocks.cutoverLibraryMigration).not.toHaveBeenCalled();
		expect(dialog.textContent).toContain(
			"old Playlist, Queue, and snapshot references are dropped",
		);
		expect(dialog.textContent).toContain("Verified copies1");

		fireEvent.click(button("Activate migrated Tracks", dialog));

		await screen.findByRole("table", { name: /Cutover: 1 migrated/ });
		await waitFor(() =>
			expect(screen.queryByRole("dialog", { name: CUTOVER_DIALOG })).toBeNull(),
		);
		screen.getByText(`New Track ID ${MIGRATED_ID}`);
		expect(invalidate).toHaveBeenCalledWith({ queryKey: ["library"] });
		expect(isDisabled(button("Copy and verify"))).toBe(true);
	});

	it("keeps the cutover dialog open with the error when activation fails", async () => {
		mocks.cutoverLibraryMigration.mockRejectedValueOnce(
			new Error("Managed Storage does not have enough capacity"),
		);
		renderSection();
		const dialog = await stageAndOpenCutover();
		fireEvent.click(button("Activate migrated Tracks", dialog));
		const alert = await within(dialog).findByRole("alert");
		expect(alert.textContent).toContain("enough capacity");
		expect(screen.queryByRole("table", { name: /Cutover:/ })).toBeNull();
	});

	it("surfaces analysis failures in the section alert", async () => {
		mocks.previewLibraryMigration.mockRejectedValueOnce(
			new Error("Library Migration analysis is already running"),
		);
		renderSection();
		fireEvent.click(
			await screen.findByRole("button", { name: "Analyze library" }),
		);
		const alert = await screen.findByRole("alert");
		expect(alert.textContent).toContain("already running");
	});

	it("requires a separate destructive confirmation for Legacy Source Cleanup", async () => {
		renderSection();
		const dialog = await openCleanup();
		within(dialog).getByText("2.00 MiB");
		expect(dialog.textContent).toContain("Files to delete1");
		expect(mocks.cleanupLibraryMigrationSources).not.toHaveBeenCalled();

		fireEvent.click(button("Delete legacy sources permanently", dialog));

		await screen.findByRole("table", { name: /Cleanup: 1 deleted/ });
		expect(mocks.cleanupLibraryMigrationSources).toHaveBeenCalledWith({
			trackIds: [MIGRATED_ID],
			fileCount: 1,
			totalSizeBytes: TWO_MIB,
		});
		expect(screen.queryByRole("dialog", { name: CLEANUP_DIALOG })).toBeNull();
	});

	it("shows a cleanup conflict inside the dialog without closing it", async () => {
		mocks.cleanupLibraryMigrationSources.mockRejectedValueOnce(
			new Error(
				"Legacy source cleanup selection no longer matches the preview",
			),
		);
		renderSection();
		const dialog = await openCleanup();
		fireEvent.click(button("Delete legacy sources permanently", dialog));
		const alert = await within(dialog).findByRole("alert");
		expect(alert.textContent).toContain("no longer matches");
		screen.getByRole("dialog", { name: CLEANUP_DIALOG });
	});

	it("disables deletion when no legacy source is eligible", async () => {
		mocks.previewLibraryMigrationCleanup.mockResolvedValue({
			eligibleCount: 0,
			ineligibleCount: 1,
			totalSizeBytes: 0,
			files: [ineligibleFile],
		});
		renderSection();
		const dialog = await openCleanup();
		expect(
			isDisabled(button("Delete legacy sources permanently", dialog)),
		).toBe(true);
		expect(dialog.textContent).toContain("No legacy source file is eligible");
	});

	it("shows the already-migrated outcome when analysis finds nothing left", async () => {
		mocks.previewLibraryMigration.mockResolvedValue({
			acceptedCount: 0,
			rejectedCount: 0,
			files: [],
		});
		renderSection();
		fireEvent.click(
			await screen.findByRole("button", { name: "Analyze library" }),
		);
		await screen.findByText(/No Legacy Tracks remain/);
		screen.getByRole("status");
		expect(isDisabled(button("Copy and verify"))).toBe(true);
		expect(screen.queryByRole("alert")).toBeNull();
	});

	it("labels Exact Duplicates and capacity rejections distinctly", async () => {
		mocks.previewLibraryMigration.mockResolvedValue({
			acceptedCount: 0,
			rejectedCount: 2,
			files: [
				{
					trackId: LEGACY_ID,
					originalFilename: "dupe.mp3",
					state: "rejected",
					errorCode: "exact_duplicate",
					errorReason: "File bytes match an existing Track",
				},
				{
					trackId: REJECTED_ID,
					originalFilename: "big.flac",
					state: "rejected",
					errorCode: "insufficient_storage",
					errorField: "capacity",
					errorReason:
						"Managed Storage does not have enough capacity for this migration and its safety reserve",
				},
			],
		});
		renderSection();
		fireEvent.click(
			await screen.findByRole("button", { name: "Analyze library" }),
		);
		const table = await screen.findByRole("table", {
			name: /Analysis: 0 accepted/,
		});
		within(table).getByText("Exact Duplicate");
		within(table).getByText("Needs space");
		const alert = screen.getByRole("alert");
		expect(alert.textContent).toContain(
			"enough free space for 1 otherwise accepted file",
		);
		expect(isDisabled(button("Copy and verify"))).toBe(true);
	});

	it("drops a cleanup error when the dialog is dismissed", async () => {
		mocks.cleanupLibraryMigrationSources.mockRejectedValueOnce(
			new Error(
				"Legacy source cleanup selection no longer matches the preview",
			),
		);
		renderSection();
		const dialog = await openCleanup();
		fireEvent.click(button("Delete legacy sources permanently", dialog));
		await within(dialog).findByRole("alert");
		fireEvent.click(button("Cancel", dialog));
		await waitFor(() =>
			expect(screen.queryByRole("dialog", { name: CLEANUP_DIALOG })).toBeNull(),
		);
		expect(screen.queryByRole("alert")).toBeNull();
	});

	it("announces progress through a polite live region", async () => {
		renderSection();
		const status = await screen.findByText(
			"Analyze the library to see which Legacy Tracks can be migrated.",
		);
		expect(status.getAttribute("aria-live")).toBe("polite");
		await analyze();
		expect(status.textContent).toContain("Review the analysis");
	});
});

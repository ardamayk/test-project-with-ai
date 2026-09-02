import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	act,
	cleanup,
	fireEvent,
	render,
	screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TracksPage } from "./-tracks-page";

const mocks = vi.hoisted(() => ({
	listTracks: vi.fn(),
	listPlaylists: vi.fn(),
	getPlaylist: vi.fn(),
	addPlaylistTrack: vi.fn(),
	removePlaylistTrack: vi.fn(),
	createManagedImportBatch: vi.fn(),
	getManagedImportBatch: vi.fn(),
	confirmManagedImportBatch: vi.fn(),
	cancelManagedImportBatch: vi.fn(),
	createManagedImportJob: vi.fn(),
	uploadManagedImportFile: vi.fn(),
	confirmManagedImport: vi.fn(),
}));

const libraryTracks = [
	{
		id: "t1",
		title: "Anti-Hero",
		artistName: "Taylor Swift",
		albumId: "a1",
		durationMs: 200_000,
		format: "flac",
		genre: "Synthpop",
	},
	{
		id: "t2",
		title: "Bad Blood",
		artistName: "Taylor Swift",
		albumId: "a2",
		durationMs: 211_000,
		format: "flac",
		genre: "Pop",
	},
];

vi.mock("#/lib/api", () => ({
	apiClient: {
		listTracks: mocks.listTracks,
		listPlaylists: mocks.listPlaylists,
		getPlaylist: mocks.getPlaylist,
		addPlaylistTrack: mocks.addPlaylistTrack,
		removePlaylistTrack: mocks.removePlaylistTrack,
		createManagedImportBatch: mocks.createManagedImportBatch,
		getManagedImportBatch: mocks.getManagedImportBatch,
		confirmManagedImportBatch: mocks.confirmManagedImportBatch,
		cancelManagedImportBatch: mocks.cancelManagedImportBatch,
		createManagedImportJob: mocks.createManagedImportJob,
		uploadManagedImportFile: mocks.uploadManagedImportFile,
		confirmManagedImport: mocks.confirmManagedImport,
	},
}));

vi.mock("#/components/track-list", () => ({
	TrackList: ({
		tracks,
		numbering,
		showFavorite,
		compact,
	}: {
		tracks: Array<{ id: string; title: string }>;
		numbering?: string;
		showFavorite?: boolean;
		compact?: boolean;
	}) => (
		<div>
			<p data-testid="track-numbering">{numbering}</p>
			<p data-testid="track-show-favorite">{String(showFavorite)}</p>
			<p data-testid="track-compact">{String(compact)}</p>
			{tracks.map((track) => (
				<p key={track.id}>{track.title}</p>
			))}
		</div>
	),
}));

function renderWithQuery(ui: React.ReactElement) {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return render(
		<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
	);
}

async function openImportMusicDialog() {
	renderWithQuery(<TracksPage />);
	await screen.findByText("Anti-Hero");
	fireEvent.click(screen.getByRole("button", { name: "Import Music" }));
}

function selectAudioFolder(files: File[]) {
	fireEvent.change(screen.getByLabelText("Audio folder"), {
		target: { files },
	});
}

function createFolderFile(
	contents: string,
	name: string,
	clientPath: string,
	type = "audio/flac",
) {
	const file = new File([contents], name, { type });
	Object.defineProperty(file, "webkitRelativePath", { value: clientPath });
	return file;
}

function selectFolderWithIgnoredFiles() {
	const track = createFolderFile(
		"flac bytes",
		"track.FLAC",
		"Collection/Disc 1/track.FLAC",
	);
	const hiddenTrack = createFolderFile(
		"hidden",
		"hidden.mp3",
		"Collection/.archive/hidden.mp3",
		"audio/mpeg",
	);
	const sidecar = createFolderFile(
		"cover",
		"cover.jpg",
		"Collection/Disc 1/cover.jpg",
		"image/jpeg",
	);
	selectAudioFolder([track, hiddenTrack, sidecar]);
	return track;
}

function createImportPreview(jobId: string) {
	return {
		jobId,
		status: "awaiting_confirmation",
		revision: 2,
		file: {
			originalFilename: `${jobId}.flac`,
			title: jobId,
			artists: ["Test Artist"],
			album: "Strict Import Tests",
		},
	};
}

function createBatchFile(jobId: string, isAccepted: boolean) {
	return {
		jobId,
		state: isAccepted ? "accepted" : "unresolved",
		status: isAccepted ? "awaiting_confirmation" : "uploading",
		revision: isAccepted ? 2 : 1,
		validationProgress: isAccepted ? 100 : 0,
		selected: isAccepted,
	};
}

function mockClientFileJobs() {
	mocks.createManagedImportJob
		.mockReset()
		.mockImplementation((_batchId, clientFileId) =>
			Promise.resolve({ id: clientFileId, status: "uploading", revision: 1 }),
		);
}

function mockDeferredUploads() {
	const releases: Array<() => void> = [];
	let activeUploads = 0;
	let maximumActiveUploads = 0;
	mocks.uploadManagedImportFile.mockImplementation(async (jobId) => {
		activeUploads++;
		maximumActiveUploads = Math.max(maximumActiveUploads, activeUploads);
		await new Promise<void>((resolve) => releases.push(resolve));
		activeUploads--;
		return createImportPreview(jobId);
	});
	return {
		releases,
		getActiveUploads: () => activeUploads,
		getMaximumActiveUploads: () => maximumActiveUploads,
	};
}

function mockRetryableBatchResponses() {
	mocks.getManagedImportBatch
		.mockResolvedValueOnce({
			id: "batch-1",
			status: "uploading",
			revision: 3,
			files: [
				createBatchFile("import-1", false),
				createBatchFile("import-2", true),
			],
		})
		.mockResolvedValueOnce({
			id: "batch-1",
			status: "uploading",
			revision: 4,
			files: [
				createBatchFile("import-1", true),
				createBatchFile("import-2", true),
			],
		});
}

function mockInterruptedFolderUpload() {
	let interruptedAttempts = 0;
	let finishRetry: (() => void) | undefined;
	mocks.uploadManagedImportFile.mockImplementation(
		(jobId, _filename, _file, onProgress) => {
			if (jobId !== "import-1")
				return Promise.resolve(createImportPreview(jobId));
			interruptedAttempts++;
			if (interruptedAttempts === 1)
				return Promise.reject(new Error("upload interrupted"));
			onProgress?.(45);
			return new Promise((resolve) => {
				finishRetry = () => resolve(createImportPreview(jobId));
			});
		},
	);
	return () => finishRetry?.();
}

function expectUploadAttempts(jobId: string, count: number) {
	expect(
		mocks.uploadManagedImportFile.mock.calls.filter(
			([currentJobId]) => currentJobId === jobId,
		),
	).toHaveLength(count);
}

describe("tracks route", () => {
	beforeEach(() => {
		mocks.listTracks.mockReset();
		mocks.createManagedImportJob.mockReset();
		mocks.createManagedImportBatch.mockReset();
		mocks.getManagedImportBatch.mockReset();
		mocks.confirmManagedImportBatch.mockReset();
		mocks.cancelManagedImportBatch.mockReset();
		mocks.uploadManagedImportFile.mockReset();
		mocks.confirmManagedImport.mockReset();
		mocks.listTracks.mockResolvedValue({
			items: libraryTracks,
		});
		mocks.listPlaylists.mockResolvedValue({
			items: [{ id: "favorites", name: "Favorites", isDefault: true }],
		});
		mocks.getPlaylist.mockResolvedValue({ tracks: [] });
		mocks.addPlaylistTrack.mockResolvedValue({ tracks: [] });
		mocks.removePlaylistTrack.mockResolvedValue({ tracks: [] });
		mocks.createManagedImportBatch.mockResolvedValue({
			id: "batch-1",
			status: "uploading",
			revision: 1,
			files: [],
		});
		mocks.createManagedImportJob
			.mockResolvedValueOnce({
				id: "import-1",
				status: "uploading",
				revision: 1,
			})
			.mockResolvedValueOnce({
				id: "import-2",
				status: "uploading",
				revision: 1,
			});
		mocks.uploadManagedImportFile.mockResolvedValue({
			jobId: "import-1",
			status: "awaiting_confirmation",
			revision: 2,
			file: {
				originalFilename: "strict-import.flac",
				title: "Inspection Fixture",
				artists: ["Test Artist"],
				albumArtists: ["Test Album Artist"],
				album: "Strict Import Tests",
				genres: ["Electronic"],
				trackNo: 3,
				discNo: 1,
				durationMs: 250,
				format: "flac",
				artworkMediaType: "image/png",
			},
		});
		mocks.confirmManagedImport.mockResolvedValue({
			jobId: "import-1",
			status: "committed",
			revision: 3,
			trackId: "imported-track",
		});
		mocks.getManagedImportBatch.mockResolvedValue({
			id: "batch-1",
			status: "uploading",
			revision: 3,
			files: [
				{
					jobId: "import-1",
					state: "accepted",
					status: "awaiting_confirmation",
					revision: 2,
					validationProgress: 100,
					selected: true,
				},
				{
					jobId: "import-2",
					state: "rejected",
					status: "failed",
					revision: 1,
					validationProgress: 100,
					originalFilename: "broken.flac",
					selected: false,
					errorCode: "invalid_metadata",
					errorReason: "TITLE is required",
				},
			],
		});
		mocks.confirmManagedImportBatch.mockResolvedValue({
			id: "batch-1",
			status: "completed",
			revision: 5,
			files: [
				{
					jobId: "import-1",
					state: "completed",
					status: "committed",
					revision: 3,
					validationProgress: 100,
					selected: true,
					outcome: "imported",
					trackId: "imported-track",
				},
				{
					jobId: "import-2",
					state: "rejected",
					status: "failed",
					revision: 1,
					validationProgress: 100,
					selected: false,
					outcome: "rejected",
					errorCode: "invalid_metadata",
					errorReason: "TITLE is required",
				},
			],
		});
		mocks.cancelManagedImportBatch.mockResolvedValue(undefined);
	});

	afterEach(() => {
		Reflect.deleteProperty(window, "__TAURI_INTERNALS__");
		vi.useRealTimers();
		cleanup();
	});

	it("keeps track search in the header and debounces API queries", async () => {
		renderWithQuery(<TracksPage />);

		await screen.findByText("Anti-Hero");
		expect(screen.getByTestId("track-numbering").textContent).toBe("list");
		expect(screen.getByTestId("track-show-favorite").textContent).toBe("true");
		expect(screen.getByTestId("track-compact").textContent).toBe("true");
		expect(mocks.listTracks).toHaveBeenCalledTimes(1);
		expect(mocks.listTracks).toHaveBeenLastCalledWith({
			limit: 200,
			q: undefined,
		});

		const input = screen.getByPlaceholderText("Search tracks…");
		const header = input.closest("header");
		expect(header).toBeTruthy();
		expect(header?.className).toContain("sticky");
		expect(header?.className).toContain("top-0");
		expect(header?.className).toContain("py-3");
		expect(
			header?.querySelector(".min-\\[1801px\\]\\:max-w-\\[1476px\\]"),
		).toBeTruthy();
		expect(screen.getByTestId("tracks-page-shell").className).toContain(
			"overflow-hidden",
		);
		expect(screen.getByTestId("tracks-page-content").className).toContain(
			"[scrollbar-width:none]",
		);
		expect(screen.getByTestId("tracks-page-content").className).toContain(
			"py-5",
		);
		expect(
			screen
				.getByTestId("tracks-page-content")
				.querySelector(".min-\\[1801px\\]\\:max-w-\\[1476px\\]"),
		).toBeTruthy();
		expect(input.className).toContain("h-11");
		expect(input.className).toContain("pl-10");
		expect(input.parentElement?.className).toContain("sm:max-w-md");

		vi.useFakeTimers();
		fireEvent.change(input, { target: { value: "blue" } });

		await act(async () => {
			vi.advanceTimersByTime(249);
		});
		expect(mocks.listTracks).toHaveBeenCalledTimes(1);

		await act(async () => {
			vi.advanceTimersByTime(1);
			await Promise.resolve();
		});

		expect(mocks.listTracks).toHaveBeenCalledWith({
			limit: 200,
			q: "blue",
		});
	});

	it("filters the current track list immediately while debounced search is pending", async () => {
		renderWithQuery(<TracksPage />);

		await screen.findByText("Anti-Hero");
		const input = screen.getByPlaceholderText("Search tracks…");

		vi.useFakeTimers();
		fireEvent.change(input, { target: { value: "anti-hero" } });

		expect(screen.getByText("Anti-Hero")).toBeTruthy();
		expect(screen.queryByText("Bad Blood")).toBeNull();
		expect(mocks.listTracks).toHaveBeenCalledTimes(1);
	});

	it("keeps filtered local results visible if the debounced search request fails", async () => {
		mocks.listTracks
			.mockResolvedValueOnce({ items: libraryTracks })
			.mockRejectedValueOnce(new Error("backend search failed"));

		renderWithQuery(<TracksPage />);

		await screen.findByText("Anti-Hero");
		const input = screen.getByPlaceholderText("Search tracks…");

		vi.useFakeTimers();
		fireEvent.change(input, { target: { value: "anti-hero" } });
		await act(async () => {
			vi.advanceTimersByTime(250);
			await Promise.resolve();
			await Promise.resolve();
		});

		expect(screen.getByText("Anti-Hero")).toBeTruthy();
		expect(screen.queryByText("Bad Blood")).toBeNull();
		expect(screen.queryByText("Failed to load tracks")).toBeNull();
	});

	it("imports supported folder audio without sending client paths", async () => {
		await openImportMusicDialog();
		const folderInput = screen.getByLabelText("Audio folder");
		expect(folderInput.getAttribute("webkitdirectory")).toBe("");
		const track = selectFolderWithIgnoredFiles();

		await vi.waitFor(() =>
			expect(mocks.uploadManagedImportFile).toHaveBeenCalledOnce(),
		);
		expect(mocks.createManagedImportJob).toHaveBeenCalledOnce();
		expect(mocks.uploadManagedImportFile).toHaveBeenCalledWith(
			"import-1",
			"track.FLAC",
			track,
			expect.any(Function),
		);
	});

	it("limits recursive folder uploads to three concurrent files", async () => {
		mockClientFileJobs();
		const uploads = mockDeferredUploads();
		await openImportMusicDialog();
		selectAudioFolder(
			["one", "two", "three", "four"].map(
				(name) => new File([name], `${name}.flac`),
			),
		);

		await vi.waitFor(() =>
			expect(mocks.uploadManagedImportFile).toHaveBeenCalledTimes(3),
		);
		expect(uploads.getMaximumActiveUploads()).toBe(3);
		uploads.releases.shift()?.();
		await vi.waitFor(() =>
			expect(mocks.uploadManagedImportFile).toHaveBeenCalledTimes(4),
		);
		for (const release of uploads.releases) release();
		await vi.waitFor(() => expect(uploads.getActiveUploads()).toBe(0));
	});

	it("retries only an interrupted folder file in its active job", async () => {
		mockRetryableBatchResponses();
		const finishRetry = mockInterruptedFolderUpload();
		await openImportMusicDialog();
		selectAudioFolder([
			new File(["one"], "one.flac"),
			new File(["two"], "two.flac"),
		]);

		await vi.waitFor(() =>
			expect(mocks.uploadManagedImportFile).toHaveBeenCalledTimes(3),
		);
		const interruptedRow = screen.getByText("one.flac").closest("article");
		expect(interruptedRow?.textContent).toContain("Unresolved");
		expect(
			interruptedRow
				?.querySelector('[role="progressbar"]')
				?.getAttribute("aria-valuenow"),
		).toBe("45");
		finishRetry();
		await vi.waitFor(() =>
			expect(interruptedRow?.textContent).toContain("Accepted"),
		);
		expectUploadAttempts("import-1", 2);
		expectUploadAttempts("import-2", 1);
	});

	it("imports a selected file and reports a rejected sibling independently", async () => {
		mocks.listTracks
			.mockResolvedValueOnce({ items: libraryTracks })
			.mockResolvedValue({
				items: [
					...libraryTracks,
					{
						id: "imported-track",
						title: "Inspection Fixture",
						artistName: "Test Artist",
						albumId: "imported-album",
						durationMs: 250,
						format: "flac",
					},
				],
			});
		renderWithQuery(<TracksPage />);
		await screen.findByText("Anti-Hero");

		fireEvent.click(screen.getByRole("button", { name: "Import Music" }));
		expect(screen.getByRole("dialog")).toBeTruthy();
		expect(screen.getByRole("heading", { name: "Import Music" })).toBeTruthy();
		const fileInput = screen.getByLabelText("Audio files");
		expect(fileInput.getAttribute("accept")).toBe(
			".flac,.mp3,.m4a,.ogg,.opus,.wav",
		);
		const acceptedFile = new File(["flac bytes"], "strict-import.flac", {
			type: "audio/flac",
		});
		const rejectedFile = new File(["broken"], "broken.flac", {
			type: "audio/flac",
		});
		mocks.uploadManagedImportFile
			.mockImplementationOnce(async (_jobId, _filename, _file, onProgress) => {
				onProgress?.(60);
				return {
					jobId: "import-1",
					status: "awaiting_confirmation",
					revision: 2,
					file: {
						originalFilename: "strict-import.flac",
						title: "Inspection Fixture",
						artists: ["Test Artist"],
						albumArtists: ["Test Album Artist"],
						album: "Strict Import Tests",
						genres: ["Electronic"],
						trackNo: 3,
						discNo: 1,
						durationMs: 250,
						format: "flac",
						artworkMediaType: "image/png",
					},
				};
			})
			.mockRejectedValueOnce(new Error("TITLE is required"));
		fireEvent.change(fileInput, {
			target: { files: [acceptedFile, rejectedFile] },
		});

		await screen.findByText("Inspection Fixture");
		expect(screen.getByText("Test Artist")).toBeTruthy();
		expect(screen.getByText("Strict Import Tests")).toBeTruthy();
		expect(mocks.createManagedImportBatch).toHaveBeenCalledOnce();
		expect(mocks.createManagedImportJob).toHaveBeenCalledTimes(2);
		expect(mocks.createManagedImportJob).toHaveBeenNthCalledWith(
			1,
			"batch-1",
			expect.any(String),
		);
		expect(mocks.uploadManagedImportFile).toHaveBeenCalledWith(
			"import-1",
			"strict-import.flac",
			acceptedFile,
			expect.any(Function),
			expect.any(AbortSignal),
		);
		expect(screen.getByText("Rejected")).toBeTruthy();
		expect(screen.getByText("TITLE is required")).toBeTruthy();
		const acceptedCheckbox = screen.getByRole("checkbox", {
			name: "Select strict-import.flac",
		});
		expect(acceptedCheckbox).not.toHaveProperty("disabled", true);
		fireEvent.click(acceptedCheckbox);
		expect(acceptedCheckbox).toHaveProperty("checked", false);
		fireEvent.click(acceptedCheckbox);
		expect(acceptedCheckbox).toHaveProperty("checked", true);
		expect(
			screen.getByRole("checkbox", { name: "Select broken.flac" }),
		).toHaveProperty("disabled", true);

		fireEvent.click(screen.getByRole("button", { name: "Confirm Import" }));

		await screen.findByText("Inspection Fixture");
		expect(mocks.confirmManagedImportBatch).toHaveBeenCalledWith("batch-1", 3, [
			"import-1",
		]);
		await vi.waitFor(() => expect(mocks.listTracks).toHaveBeenCalledTimes(2));
		expect(screen.getByText("Imported")).toBeTruthy();
		expect(screen.getByRole("dialog")).toBeTruthy();
	});

	it("retains the created batch when the preview refresh fails", async () => {
		mocks.getManagedImportBatch
			.mockRejectedValueOnce(new Error("preview refresh unavailable"))
			.mockResolvedValueOnce({
				id: "batch-1",
				status: "uploading",
				revision: 3,
				files: [
					{
						jobId: "import-1",
						state: "accepted",
						status: "awaiting_confirmation",
						revision: 2,
						validationProgress: 100,
						selected: true,
					},
				],
			});
		mocks.confirmManagedImportBatch.mockResolvedValueOnce({
			id: "batch-1",
			status: "completed",
			revision: 5,
			files: [
				{
					jobId: "import-1",
					state: "completed",
					status: "committed",
					revision: 3,
					validationProgress: 100,
					selected: true,
					outcome: "imported",
					trackId: "imported-track",
				},
			],
		});
		renderWithQuery(<TracksPage />);
		await screen.findByText("Anti-Hero");
		fireEvent.click(screen.getByRole("button", { name: "Import Music" }));
		const fileInput = screen.getByLabelText("Audio files");
		expect(fileInput.getAttribute("accept")).toBe(
			".flac,.mp3,.m4a,.ogg,.opus,.wav",
		);
		fireEvent.change(fileInput, {
			target: {
				files: [
					new File(["flac bytes"], "strict-import.flac", {
						type: "audio/flac",
					}),
				],
			},
		});

		await screen.findByText("preview refresh unavailable");
		const confirmButton = screen.getByRole("button", {
			name: "Confirm Import",
		});
		expect(confirmButton).not.toHaveProperty("disabled", true);
		fireEvent.click(confirmButton);

		await screen.findByText("Imported");
		expect(mocks.getManagedImportBatch).toHaveBeenCalledTimes(2);
		expect(mocks.confirmManagedImportBatch).toHaveBeenCalledWith("batch-1", 3, [
			"import-1",
		]);
	});

	it("confirms modal close and cancels uncommitted server staging", async () => {
		const confirmClose = vi.spyOn(window, "confirm").mockReturnValue(true);
		renderWithQuery(<TracksPage />);
		await screen.findByText("Anti-Hero");
		fireEvent.click(screen.getByRole("button", { name: "Import Music" }));
		fireEvent.change(screen.getByLabelText("Audio files"), {
			target: {
				files: [
					new File(["flac bytes"], "strict-import.flac", {
						type: "audio/flac",
					}),
				],
			},
		});
		await screen.findByText("Inspection Fixture");

		fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

		await vi.waitFor(() =>
			expect(mocks.cancelManagedImportBatch).toHaveBeenCalledWith("batch-1"),
		);
		expect(confirmClose).toHaveBeenCalledOnce();
		await vi.waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
		confirmClose.mockRestore();
	});

	it("aborts active uploads before canceling their server batch", async () => {
		const confirmClose = vi.spyOn(window, "confirm").mockReturnValue(true);
		let uploadSignal: AbortSignal | undefined;
		mocks.uploadManagedImportFile.mockImplementationOnce(
			(_jobId, _filename, _file, _onProgress, signal: AbortSignal) => {
				uploadSignal = signal;
				return new Promise((_resolve, reject) => {
					signal.addEventListener("abort", () =>
						reject(new DOMException("canceled", "AbortError")),
					);
				});
			},
		);
		renderWithQuery(<TracksPage />);
		await screen.findByText("Anti-Hero");
		fireEvent.click(screen.getByRole("button", { name: "Import Music" }));
		fireEvent.change(screen.getByLabelText("Audio files"), {
			target: {
				files: [
					new File(["flac bytes"], "active.flac", {
						type: "audio/flac",
					}),
				],
			},
		});
		await vi.waitFor(() =>
			expect(mocks.uploadManagedImportFile).toHaveBeenCalledOnce(),
		);

		fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

		await vi.waitFor(() => expect(uploadSignal?.aborted).toBe(true));
		expect(mocks.cancelManagedImportBatch).toHaveBeenCalledWith("batch-1");
		await vi.waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
		confirmClose.mockRestore();
	});

	it("keeps uncommitted import open when modal close is not confirmed", async () => {
		const confirmClose = vi.spyOn(window, "confirm").mockReturnValue(false);
		renderWithQuery(<TracksPage />);
		await screen.findByText("Anti-Hero");
		fireEvent.click(screen.getByRole("button", { name: "Import Music" }));
		fireEvent.change(screen.getByLabelText("Audio files"), {
			target: {
				files: [
					new File(["flac bytes"], "strict-import.flac", {
						type: "audio/flac",
					}),
				],
			},
		});
		await screen.findByText("Inspection Fixture");

		fireEvent.click(screen.getByRole("button", { name: "Close Import Music" }));

		expect(confirmClose).toHaveBeenCalledOnce();
		expect(mocks.cancelManagedImportBatch).not.toHaveBeenCalled();
		expect(screen.getByRole("dialog")).toBeTruthy();
		confirmClose.mockRestore();
	});

	it("reconciles an accepted upload whose response was lost", async () => {
		mocks.uploadManagedImportFile.mockRejectedValueOnce(
			new Error("upload response lost"),
		);
		mocks.getManagedImportBatch
			.mockRejectedValueOnce(new Error("preview refresh unavailable"))
			.mockResolvedValueOnce({
				id: "batch-1",
				status: "uploading",
				revision: 3,
				files: [
					{
						jobId: "import-1",
						state: "accepted",
						status: "awaiting_confirmation",
						revision: 2,
						validationProgress: 100,
						selected: true,
					},
				],
			});
		renderWithQuery(<TracksPage />);
		await screen.findByText("Anti-Hero");
		fireEvent.click(screen.getByRole("button", { name: "Import Music" }));
		fireEvent.change(screen.getByLabelText("Audio files"), {
			target: {
				files: [
					new File(["flac bytes"], "strict-import.flac", {
						type: "audio/flac",
					}),
				],
			},
		});
		await screen.findByText("preview refresh unavailable");

		fireEvent.click(screen.getByRole("button", { name: "Confirm Import" }));

		await vi.waitFor(() =>
			expect(mocks.confirmManagedImportBatch).toHaveBeenCalledWith(
				"batch-1",
				3,
				["import-1"],
			),
		);
		expect(screen.queryByText("upload response lost")).toBeNull();
	});

	it("retries an unresolved server job whose create response was lost", async () => {
		let clientFileId = "";
		mocks.createManagedImportJob
			.mockReset()
			.mockImplementationOnce((_batchId, currentClientFileId) => {
				clientFileId = currentClientFileId;
				return Promise.reject(new Error("job response lost"));
			});
		mocks.getManagedImportBatch
			.mockImplementationOnce(async () => ({
				id: "batch-1",
				status: "uploading",
				revision: 2,
				files: [
					{
						jobId: "server-import-1",
						clientFileId,
						state: "unresolved",
						status: "uploading",
						revision: 1,
						validationProgress: 0,
						selected: false,
					},
				],
			}))
			.mockResolvedValueOnce({
				id: "batch-1",
				status: "uploading",
				revision: 3,
				files: [
					{
						jobId: "server-import-1",
						state: "accepted",
						status: "awaiting_confirmation",
						revision: 2,
						validationProgress: 100,
						selected: true,
					},
				],
			});
		renderWithQuery(<TracksPage />);
		await screen.findByText("Anti-Hero");
		fireEvent.click(screen.getByRole("button", { name: "Import Music" }));
		const file = new File(["flac bytes"], "strict-import.flac", {
			type: "audio/flac",
		});
		fireEvent.change(screen.getByLabelText("Audio files"), {
			target: { files: [file] },
		});

		await vi.waitFor(() =>
			expect(mocks.uploadManagedImportFile).toHaveBeenCalledWith(
				"server-import-1",
				"strict-import.flac",
				file,
				expect.any(Function),
				undefined,
			),
		);
		expect(await screen.findByText("Accepted")).toBeDefined();
		expect(screen.queryByText("job response lost")).toBeNull();
	});

	it("correlates a recovered job after an earlier create genuinely fails", async () => {
		let recoveredClientFileId = "";
		mocks.createManagedImportJob
			.mockReset()
			.mockRejectedValueOnce(new Error("first create failed"))
			.mockImplementationOnce((_batchId, clientFileId) => {
				recoveredClientFileId = clientFileId;
				return Promise.reject(new Error("second response lost"));
			});
		mocks.getManagedImportBatch
			.mockImplementationOnce(async () => ({
				id: "batch-1",
				status: "uploading" as const,
				revision: 2,
				files: [
					{
						jobId: "server-import-2",
						clientFileId: recoveredClientFileId,
						state: "unresolved" as const,
						status: "uploading" as const,
						revision: 1,
						validationProgress: 0,
						selected: false,
					},
				],
			}))
			.mockResolvedValueOnce({
				id: "batch-1",
				status: "uploading",
				revision: 3,
				files: [],
			});
		renderWithQuery(<TracksPage />);
		await screen.findByText("Anti-Hero");
		fireEvent.click(screen.getByRole("button", { name: "Import Music" }));
		const firstFile = new File(["first"], "first.flac", {
			type: "audio/flac",
		});
		const secondFile = new File(["second"], "second.flac", {
			type: "audio/flac",
		});
		fireEvent.change(screen.getByLabelText("Audio files"), {
			target: { files: [firstFile, secondFile] },
		});

		await vi.waitFor(() =>
			expect(mocks.uploadManagedImportFile).toHaveBeenCalled(),
		);
		expect(mocks.uploadManagedImportFile).toHaveBeenCalledWith(
			"server-import-2",
			"second.flac",
			secondFile,
			expect.any(Function),
			undefined,
		);
		expect(mocks.uploadManagedImportFile).not.toHaveBeenCalledWith(
			"server-import-2",
			"first.flac",
			firstFile,
			expect.any(Function),
			undefined,
		);
	});

	it("preserves an explicit deselection across confirmation refresh", async () => {
		renderWithQuery(<TracksPage />);
		await screen.findByText("Anti-Hero");
		fireEvent.click(screen.getByRole("button", { name: "Import Music" }));
		fireEvent.change(screen.getByLabelText("Audio files"), {
			target: {
				files: [
					new File(["flac bytes"], "strict-import.flac", {
						type: "audio/flac",
					}),
				],
			},
		});
		const checkbox = await screen.findByRole("checkbox", {
			name: "Select strict-import.flac",
		});
		fireEvent.click(checkbox);
		fireEvent.click(screen.getByRole("button", { name: "Confirm Import" }));

		await vi.waitFor(() =>
			expect(mocks.confirmManagedImportBatch).toHaveBeenCalledWith(
				"batch-1",
				3,
				[],
			),
		);
	});

	it("freezes file selection while confirmation is pending", async () => {
		let finishConfirmation: (() => void) | undefined;
		mocks.confirmManagedImportBatch.mockImplementationOnce(
			() =>
				new Promise((resolve) => {
					finishConfirmation = () =>
						resolve({
							id: "batch-1",
							status: "completed",
							revision: 4,
							files: [],
						});
				}),
		);
		renderWithQuery(<TracksPage />);
		await screen.findByText("Anti-Hero");
		fireEvent.click(screen.getByRole("button", { name: "Import Music" }));
		fireEvent.change(screen.getByLabelText("Audio files"), {
			target: {
				files: [
					new File(["flac bytes"], "strict-import.flac", {
						type: "audio/flac",
					}),
				],
			},
		});
		const checkbox = await screen.findByRole("checkbox", {
			name: "Select strict-import.flac",
		});
		fireEvent.click(screen.getByRole("button", { name: "Confirm Import" }));
		await vi.waitFor(() => expect(checkbox).toHaveProperty("disabled", true));
		finishConfirmation?.();
	});

	it("keeps selection frozen when the server batch remains confirming", async () => {
		mocks.getManagedImportBatch
			.mockResolvedValueOnce({
				id: "batch-1",
				status: "uploading",
				revision: 3,
				files: [
					{
						jobId: "import-1",
						state: "accepted",
						status: "awaiting_confirmation",
						revision: 2,
						validationProgress: 100,
						selected: true,
					},
				],
			})
			.mockResolvedValueOnce({
				id: "batch-1",
				status: "confirming",
				revision: 4,
				files: [
					{
						jobId: "import-1",
						state: "accepted",
						status: "awaiting_confirmation",
						revision: 2,
						validationProgress: 100,
						selected: true,
					},
				],
			});
		mocks.confirmManagedImportBatch.mockRejectedValueOnce(
			new Error("confirmation response lost"),
		);
		renderWithQuery(<TracksPage />);
		await screen.findByText("Anti-Hero");
		fireEvent.click(screen.getByRole("button", { name: "Import Music" }));
		fireEvent.change(screen.getByLabelText("Audio files"), {
			target: {
				files: [
					new File(["flac bytes"], "strict-import.flac", {
						type: "audio/flac",
					}),
				],
			},
		});
		const checkbox = await screen.findByRole("checkbox", {
			name: "Select strict-import.flac",
		});
		fireEvent.click(screen.getByRole("button", { name: "Confirm Import" }));
		await screen.findByText("confirmation response lost");
		expect(checkbox).toHaveProperty("disabled", true);
		expect(
			screen.getByRole("button", { name: "Close Import Music" }),
		).toHaveProperty("disabled", true);
		expect(screen.getByRole("button", { name: "Cancel" })).toHaveProperty(
			"disabled",
			true,
		);
	});

	it("uploads one desktop file at a time", async () => {
		Object.defineProperty(window, "__TAURI_INTERNALS__", {
			configurable: true,
			value: {},
		});
		mocks.createManagedImportJob
			.mockReset()
			.mockResolvedValueOnce({
				id: "import-1",
				status: "uploading",
				revision: 1,
			})
			.mockResolvedValueOnce({
				id: "import-2",
				status: "uploading",
				revision: 1,
			})
			.mockResolvedValueOnce({
				id: "import-3",
				status: "uploading",
				revision: 1,
			});
		const releases: Array<() => void> = [];
		let activeUploads = 0;
		let maximumActiveUploads = 0;
		mocks.uploadManagedImportFile.mockImplementation(async (jobId) => {
			activeUploads++;
			maximumActiveUploads = Math.max(maximumActiveUploads, activeUploads);
			await new Promise<void>((resolve) => releases.push(resolve));
			activeUploads--;
			return {
				jobId,
				status: "awaiting_confirmation",
				revision: 2,
				file: {
					originalFilename: `${jobId}.flac`,
					title: jobId,
					artists: ["Test Artist"],
					album: "Strict Import Tests",
				},
			};
		});
		renderWithQuery(<TracksPage />);
		await screen.findByText("Anti-Hero");
		fireEvent.click(screen.getByRole("button", { name: "Import Music" }));
		fireEvent.change(screen.getByLabelText("Audio files"), {
			target: {
				files: [
					new File(["one"], "one.flac"),
					new File(["two"], "two.flac"),
					new File(["three"], "three.flac"),
				],
			},
		});

		await vi.waitFor(() =>
			expect(mocks.uploadManagedImportFile).toHaveBeenCalledTimes(1),
		);
		releases.shift()?.();
		await vi.waitFor(() =>
			expect(mocks.uploadManagedImportFile).toHaveBeenCalledTimes(2),
		);
		releases.shift()?.();
		await vi.waitFor(() =>
			expect(mocks.uploadManagedImportFile).toHaveBeenCalledTimes(3),
		);
		releases.shift()?.();
		await vi.waitFor(() => expect(activeUploads).toBe(0));
		expect(maximumActiveUploads).toBe(1);
	});
});

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

describe("tracks route", () => {
	beforeEach(() => {
		mocks.listTracks.mockReset();
		mocks.createManagedImportJob.mockReset();
		mocks.createManagedImportBatch.mockReset();
		mocks.getManagedImportBatch.mockReset();
		mocks.confirmManagedImportBatch.mockReset();
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
	});

	afterEach(() => {
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
		fireEvent.change(screen.getByLabelText("Audio files"), {
			target: { files: [acceptedFile, rejectedFile] },
		});

		await screen.findByText("Inspection Fixture");
		expect(screen.getByText("Test Artist")).toBeTruthy();
		expect(screen.getByText("Strict Import Tests")).toBeTruthy();
		expect(mocks.createManagedImportBatch).toHaveBeenCalledOnce();
		expect(mocks.createManagedImportJob).toHaveBeenCalledTimes(2);
		expect(mocks.createManagedImportJob).toHaveBeenNthCalledWith(1, "batch-1");
		expect(mocks.uploadManagedImportFile).toHaveBeenCalledWith(
			"import-1",
			"strict-import.flac",
			acceptedFile,
			expect.any(Function),
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
		expect(fileInput.getAttribute("accept")).toBe(".flac,.mp3,.m4a,.ogg,.opus");
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
});

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AlbumDetailContent } from "./$albumId";

const mocks = vi.hoisted(() => ({
	getAlbum: vi.fn(),
	playTrack: vi.fn(),
	queueTracks: vi.fn(),
}));

vi.mock("#/lib/api", () => ({
	apiClient: {
		getAlbum: mocks.getAlbum,
		getAlbumCoverUrl: (albumId: string) => `/cover/${albumId}`,
		listAlbums: vi.fn().mockResolvedValue({ items: [] }),
	},
}));

vi.mock("@repo/ui", () => ({
	AlbumArt: ({
		coverUrl,
		className,
	}: {
		coverUrl?: string | null;
		className?: string;
	}) => (coverUrl ? <img alt="" src={coverUrl} className={className} /> : null),
	usePlayback: () => ({
		playTrack: mocks.playTrack,
		queueTracks: mocks.queueTracks,
	}),
}));

function renderWithQuery(ui: React.ReactElement) {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return render(
		<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
	);
}

describe("album detail route", () => {
	beforeEach(() => {
		mocks.getAlbum.mockResolvedValue({
			id: "album-1",
			title: "1989",
			artistId: "artist-1",
			artistName: "Taylor Swift",
			year: 2014,
			trackCount: 1,
			durationMs: 180_000,
			genres: ["Pop"],
			tracks: [
				{
					id: "track-1",
					title: "Style",
					artistName: "Taylor Swift",
					albumId: "album-1",
					durationMs: 180_000,
					format: "flac",
				},
			],
		});
	});

	afterEach(() => {
		cleanup();
	});

	it("renders album detail without the back to library link", async () => {
		renderWithQuery(<AlbumDetailContent albumId="album-1" />);

		await screen.findByRole("heading", { name: "1989" });
		expect(screen.queryByRole("link", { name: /Back to library/ })).toBeNull();
	});
});

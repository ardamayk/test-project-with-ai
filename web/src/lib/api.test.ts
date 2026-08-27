import { describe, expect, it, vi } from "vitest";

const { listenForQueueEventsMock, unlistenQueueEventsMock } = vi.hoisted(
	() => ({
		listenForQueueEventsMock: vi.fn(),
		unlistenQueueEventsMock: vi.fn(),
	}),
);

vi.mock("#/desktop/bridge", () => ({
	desktopFetch: vi.fn(),
	getCoverBaseUrl: () => "http://earthly-media.localhost",
	getMediaProxyBaseUrl: () => "http://127.0.0.1:41000/token-1",
	isDesktopClient: () => true,
	listenForQueueEvents: listenForQueueEventsMock,
}));

import { apiClient } from "./api";

describe("desktop API client", () => {
	it("uses native Queue event subscription", async () => {
		listenForQueueEventsMock.mockResolvedValue(unlistenQueueEventsMock);

		const unsubscribe = apiClient.subscribePlaybackQueueEvents(vi.fn());
		await vi.waitFor(() => expect(listenForQueueEventsMock).toHaveBeenCalled());
		unsubscribe();

		expect(unlistenQueueEventsMock).toHaveBeenCalledOnce();
	});

	it("routes covers through the bounded protocol and streams through the proxy", () => {
		expect(apiClient.getAlbumCoverUrl("album-1")).toBe(
			"http://earthly-media.localhost/api/v1/library/albums/album-1/cover",
		);
		expect(apiClient.getTrackStreamUrl("track-1")).toBe(
			"http://127.0.0.1:41000/token-1/api/v1/tracks/track-1/stream",
		);
		expect(apiClient.getRadioStationStreamUrl("station-1")).toBe(
			"http://127.0.0.1:41000/token-1/api/v1/radio/stations/station-1/stream",
		);
		expect(apiClient.getRadioCatalogPreviewStreamUrl("preview-1")).toBe(
			"http://127.0.0.1:41000/token-1/api/v1/radio/preview/preview-1/stream",
		);
	});
});

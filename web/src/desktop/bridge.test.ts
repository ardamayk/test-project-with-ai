import { beforeEach, describe, expect, it, vi } from "vitest";

const { convertFileSrcMock, invokeMock, listenMock } = vi.hoisted(() => ({
	convertFileSrcMock: vi.fn(),
	invokeMock: vi.fn(),
	listenMock: vi.fn(),
}));

vi.mock("@tauri-apps/api/core", () => ({
	convertFileSrc: convertFileSrcMock,
	invoke: invokeMock,
}));
vi.mock("@tauri-apps/api/event", () => ({ listen: listenMock }));

import {
	desktopFetch,
	getCoverBaseUrl,
	getMediaProxyBaseUrl,
	getServerConnection,
	initializeMediaProxy,
	listenForQueueEvents,
	listenForServerConnectionChanges,
	saveServerConnection,
	testServerConnection,
} from "./bridge";

describe("desktop bridge", () => {
	beforeEach(() => {
		invokeMock.mockReset();
		convertFileSrcMock.mockReset();
		listenMock.mockReset();
	});

	it("builds a platform-compatible cover protocol base", () => {
		convertFileSrcMock.mockReturnValue("http://earthly-media.localhost/");

		expect(getCoverBaseUrl()).toBe("http://earthly-media.localhost");
		expect(convertFileSrcMock).toHaveBeenCalledWith("", "earthly-media");
	});

	it("exposes renderer connection commands", async () => {
		invokeMock.mockResolvedValue({
			origin: "http://127.0.0.1:8090",
			version: "0.1.0",
			capabilities: ["api.v1"],
		});

		await getServerConnection();
		await testServerConnection("http://127.0.0.1:8090");
		await saveServerConnection("http://127.0.0.1:8090");

		expect(invokeMock).toHaveBeenNthCalledWith(1, "get_server_connection");
		expect(invokeMock).toHaveBeenNthCalledWith(2, "test_server_connection", {
			origin: "http://127.0.0.1:8090",
		});
		expect(invokeMock).toHaveBeenNthCalledWith(3, "save_server_connection", {
			origin: "http://127.0.0.1:8090",
		});
	});

	it("adapts desktop HTTP responses to fetch", async () => {
		invokeMock.mockResolvedValue({
			status: 200,
			headers: { "content-type": "application/json" },
			body: Array.from(new TextEncoder().encode('{"status":"ok"}')),
		});

		const response = await desktopFetch("/api/v1/health", {
			headers: { Accept: "application/json" },
		});

		await expect(response.json()).resolves.toEqual({ status: "ok" });
		expect(invokeMock).toHaveBeenCalledWith("desktop_http_request", {
			request: {
				method: "GET",
				url: "/api/v1/health",
				headers: { accept: "application/json" },
				body: null,
			},
		});
	});

	it("adapts bodyless desktop HTTP responses", async () => {
		invokeMock.mockResolvedValue({ status: 204, headers: {}, body: [] });

		const response = await desktopFetch("/api/v1/radio/stations/station-1", {
			method: "DELETE",
		});

		expect(response.status).toBe(204);
		expect(await response.text()).toBe("");
	});

	it("subscribes to the server connection event", async () => {
		const callback = vi.fn();
		listenMock.mockResolvedValue(vi.fn());

		await listenForServerConnectionChanges(callback);

		expect(listenMock).toHaveBeenCalledWith(
			"server-connection-changed",
			expect.any(Function),
		);
	});

	it("subscribes to native Queue events and errors", async () => {
		const listeners = new Map<string, (event: { payload: unknown }) => void>();
		const unlistenQueue = vi.fn();
		const unlistenError = vi.fn();
		listenMock.mockImplementation(
			async (name: string, callback: (event: { payload: unknown }) => void) => {
				listeners.set(name, callback);
				return name === "desktop-queue-invalidated"
					? unlistenQueue
					: unlistenError;
			},
		);
		const onEvent = vi.fn();
		const onError = vi.fn();

		const unsubscribe = await listenForQueueEvents(onEvent, onError);
		expect(invokeMock).toHaveBeenCalledWith("desktop_reconnect_queue_events");
		listeners.get("desktop-queue-invalidated")?.({
			payload: { revision: "opaque-2", sequence: "2", invalidates: ["queue"] },
		});
		listeners.get("desktop-queue-events-error")?.({
			payload: "connection lost",
		});

		expect(onEvent).toHaveBeenCalledWith({
			revision: "opaque-2",
			sequence: "2",
			invalidates: ["queue"],
		});
		expect(onError).toHaveBeenCalledWith(
			new Error("Desktop Queue event stream: connection lost"),
		);
		unsubscribe();
		expect(unlistenQueue).toHaveBeenCalledOnce();
		expect(unlistenError).toHaveBeenCalledOnce();
	});

	it("initializes the tokenized loopback media proxy URL", async () => {
		const proxyUrl = `http://127.0.0.1:41000/${"a".repeat(64)}`;
		invokeMock.mockResolvedValue(proxyUrl);

		await expect(initializeMediaProxy()).resolves.toBe(proxyUrl);
		expect(getMediaProxyBaseUrl()).toBe(proxyUrl);
		expect(invokeMock).toHaveBeenCalledWith("get_media_proxy_url");
	});
});

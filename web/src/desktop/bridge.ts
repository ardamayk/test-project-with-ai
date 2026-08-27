import { convertFileSrc, invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";

const CONNECTION_CHANGED_EVENT = "server-connection-changed";
const COVER_PROTOCOL = "earthly-media";
let mediaProxyBaseUrl: string | null = null;

export type ServerConnection = {
	origin: string;
};

export type ConnectionCheck = ServerConnection & {
	version: string;
	capabilities: string[];
};

type BridgeHttpResponse = {
	status: number;
	headers: Record<string, string>;
	body: number[];
};

export function isDesktopClient(): boolean {
	return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

export function getServerConnection(): Promise<ServerConnection | null> {
	return invoke("get_server_connection");
}

export function testServerConnection(origin: string): Promise<ConnectionCheck> {
	return invoke("test_server_connection", { origin });
}

export function saveServerConnection(origin: string): Promise<ConnectionCheck> {
	return invoke("save_server_connection", { origin });
}

export async function initializeMediaProxy(): Promise<string> {
	const value = await invoke<string>("get_media_proxy_url");
	const url = new URL(value);
	const isValid =
		url.protocol === "http:" &&
		url.hostname === "127.0.0.1" &&
		url.port.length > 0 &&
		/^\/[a-f0-9]{64}$/.test(url.pathname) &&
		url.search === "" &&
		url.hash === "";
	if (!isValid) {
		throw new TypeError("Desktop media proxy returned an invalid loopback URL");
	}
	mediaProxyBaseUrl = url.toString().replace(/\/$/, "");
	return mediaProxyBaseUrl;
}

export function getMediaProxyBaseUrl(): string {
	if (!mediaProxyBaseUrl) {
		throw new Error("Desktop media proxy is not initialized");
	}
	return mediaProxyBaseUrl;
}

export function getCoverBaseUrl(): string {
	return convertFileSrc("", COVER_PROTOCOL).replace(/\/$/, "");
}

export function listenForServerConnectionChanges(
	callback: (connection: ConnectionCheck) => void,
): Promise<UnlistenFn> {
	return listen<ConnectionCheck>(CONNECTION_CHANGED_EVENT, (event) => {
		callback(event.payload);
	});
}

export async function desktopFetch(
	input: RequestInfo | URL,
	init?: RequestInit,
): Promise<Response> {
	const url = input instanceof Request ? input.url : input.toString();
	const headers = new Headers(
		init?.headers ?? (input instanceof Request ? input.headers : undefined),
	);
	const normalizedHeaders = Object.fromEntries(
		Array.from(headers.entries(), ([name, value]) => [
			name.toLowerCase(),
			value,
		]),
	);
	const body = await encodeBody(init?.body);
	const response = await invoke<BridgeHttpResponse>("desktop_http_request", {
		request: {
			method: init?.method ?? (input instanceof Request ? input.method : "GET"),
			url,
			headers: normalizedHeaders,
			body,
		},
	});

	const isBodyless = [204, 205, 304].includes(response.status);
	return new Response(isBodyless ? null : new Uint8Array(response.body), {
		status: response.status,
		headers: response.headers,
	});
}

async function encodeBody(
	body: BodyInit | null | undefined,
): Promise<number[] | null> {
	if (body == null) return null;
	if (typeof body === "string")
		return Array.from(new TextEncoder().encode(body));
	if (body instanceof URLSearchParams) {
		return Array.from(new TextEncoder().encode(body.toString()));
	}
	if (body instanceof Blob)
		return Array.from(new Uint8Array(await body.arrayBuffer()));
	if (body instanceof ArrayBuffer) return Array.from(new Uint8Array(body));
	if (ArrayBuffer.isView(body)) {
		return Array.from(
			new Uint8Array(body.buffer, body.byteOffset, body.byteLength),
		);
	}
	throw new TypeError(
		"Desktop HTTP transport does not support streaming or FormData bodies",
	);
}

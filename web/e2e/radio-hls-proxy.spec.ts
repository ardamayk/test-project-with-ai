import { expect, test } from "@playwright/test";

type BrowserPlaybackEngineModule =
	typeof import("../src/playback/BrowserPlaybackEngine");

test("browser HLS playback completes nested requests through Music Server proxy", async ({
	page,
}) => {
	const proxyUrl = process.env.HLS_PROXY_URL;
	const upstreamOrigin = process.env.HLS_UPSTREAM_ORIGIN;
	if (!proxyUrl || !upstreamOrigin) {
		throw new Error("HLS_PROXY_URL and HLS_UPSTREAM_ORIGIN are required");
	}
	const proxyOrigin = new URL(proxyUrl).origin;
	const observedHLSRequests: string[] = [];
	page.on("request", (request) => {
		const requestUrl = request.url();
		if (
			requestUrl.startsWith(proxyOrigin) ||
			requestUrl.startsWith(upstreamOrigin)
		) {
			observedHLSRequests.push(requestUrl);
		}
	});
	const segmentResponse = page.waitForResponse(
		(response) =>
			response.url().startsWith(proxyOrigin) &&
			response.headers()["content-type"]?.startsWith("video/mp2t") === true,
	);

	await page.goto("/e2e/fixtures/hls.html");
	await page.evaluate(async (playbackUrl) => {
		const browserPlaybackEngineModulePath: string =
			"/src/playback/BrowserPlaybackEngine.ts";
		const browserPlaybackEngineModule: BrowserPlaybackEngineModule =
			await import(browserPlaybackEngineModulePath);
		const { BrowserPlaybackEngine } = browserPlaybackEngineModule;
		const engine = new BrowserPlaybackEngine();
		Object.assign(window, { hlsFixtureEngine: engine });
		void engine
			.play({
				type: "radio-station",
				station: {
					id: "fixture-station",
					name: "HLS Fixture",
					streamUrl: "http://hls-fixture.example/master.m3u8",
					tags: [],
					source: "manual",
					isFavorite: false,
					position: 0,
				},
				playbackUrl,
				sourceUrl: "http://hls-fixture.example/master.m3u8",
			})
			.catch(() => undefined);
	}, proxyUrl);

	const response = await segmentResponse;
	expect(response.ok()).toBe(true);
	await expect
		.poll(
			() =>
				observedHLSRequests.filter((requestUrl) =>
					requestUrl.includes("resource="),
				).length,
		)
		.toBeGreaterThanOrEqual(2);
	expect(
		observedHLSRequests.every((requestUrl) =>
			requestUrl.startsWith(proxyOrigin),
		),
	).toBe(true);
	expect(
		observedHLSRequests.some((requestUrl) =>
			requestUrl.startsWith(upstreamOrigin),
		),
	).toBe(false);
});

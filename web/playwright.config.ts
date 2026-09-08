import path from "node:path";
import { defineConfig, devices } from "@playwright/test";
import { getTestArtifacts, getTestRunDirectory } from "./testing/storage";

const runDirectory = getTestRunDirectory();
const managedStorage = runDirectory
	? path.join(runDirectory, "web/data/managed")
	: "./data/e2e-managed";
if (runDirectory) process.env.MANAGED_IMPORT_TEST_STORAGE_PATH = managedStorage;

export default defineConfig({
	testDir: "./e2e",
	testIgnore: ["radio-hls-proxy.spec.ts", "production-smoke.spec.ts"],
	fullyParallel: true,
	forbidOnly: !!process.env.CI,
	// CI diagnostics for the Integration Gate: one retry, and on failure an
	// HTML report plus traces, screenshots, and retry data (issue #79).
	retries: process.env.CI ? 1 : 0,
	// Import journeys inspect the same server staging directory.
	workers: process.env.CI || runDirectory ? 1 : undefined,
	...getTestArtifacts("web"),
	use: {
		baseURL: process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:3000",
		trace: "on-first-retry",
		screenshot: "only-on-failure",
	},
	projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
	webServer: [
		{
			command: "go run ./cmd/server",
			timeout: 120_000,
			url: "http://localhost:8090/api/v1/health",
			reuseExistingServer: runDirectory ? false : !process.env.CI,
			cwd: "../server",
			// Pass the same isolated path to the server and staging assertions.
			env: {
				SERVER_ADDR: "127.0.0.1:8090",
				DATABASE_PATH: runDirectory
					? path.join(runDirectory, "web/data/e2e.db")
					: "./data/e2e.db",
				MANAGED_STORAGE_PATH: managedStorage,
			},
		},
		{
			command: "pnpm dev",
			timeout: 120_000,
			url: "http://localhost:3000",
			reuseExistingServer: runDirectory ? false : !process.env.CI,
		},
	],
});

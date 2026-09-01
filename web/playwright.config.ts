import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
	testDir: "./e2e",
	testIgnore: ["radio-hls-proxy.spec.ts", "production-smoke.spec.ts"],
	fullyParallel: true,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	workers: process.env.CI ? 1 : undefined,
	reporter: "list",
	use: {
		baseURL: process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:3000",
		trace: "on-first-retry",
	},
	projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
	webServer: [
		{
			command: "go run ./cmd/server",
			url: "http://localhost:8090/api/v1/health",
			reuseExistingServer: !process.env.CI,
			cwd: "../server",
			env: { SERVER_ADDR: "127.0.0.1:8090", DATABASE_PATH: "./data/e2e.db" },
		},
		{
			command: "pnpm dev",
			url: "http://localhost:3000",
			reuseExistingServer: !process.env.CI,
		},
	],
});

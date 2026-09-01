import { defineConfig, devices } from "@playwright/test";

const PRODUCTION_API_PORT = 18090;
const PRODUCTION_API_ORIGIN = `http://127.0.0.1:${PRODUCTION_API_PORT}`;

export default defineConfig({
	testDir: "./e2e",
	testMatch: "production-smoke.spec.ts",
	fullyParallel: false,
	forbidOnly: !!process.env.CI,
	retries: 0,
	workers: 1,
	timeout: 15_000,
	expect: { timeout: 5_000 },
	reporter: "list",
	use: {
		baseURL: "http://127.0.0.1:4173",
		trace: "on-first-retry",
	},
	projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
	webServer: [
		{
			command: "go run ./cmd/server",
			url: `${PRODUCTION_API_ORIGIN}/api/v1/health`,
			cwd: "../server",
			env: {
				SERVER_ADDR: `127.0.0.1:${PRODUCTION_API_PORT}`,
				DATABASE_PATH: "./data/e2e-production.db",
			},
			reuseExistingServer: false,
		},
		{
			command: "pnpm build && pnpm preview --host 127.0.0.1 --port 4173",
			url: "http://127.0.0.1:4173",
			env: { VITE_PROXY_TARGET: PRODUCTION_API_ORIGIN },
			reuseExistingServer: false,
		},
	],
});

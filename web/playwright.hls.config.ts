import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
	testDir: "./e2e",
	fullyParallel: false,
	workers: 1,
	reporter: "list",
	use: {
		baseURL: "http://127.0.0.1:3417",
		trace: "retain-on-failure",
		...devices["Desktop Chrome"],
		launchOptions: {
			args: ["--autoplay-policy=no-user-gesture-required"],
		},
	},
	webServer: {
		command: "pnpm exec vite dev --host 127.0.0.1 --port 3417 --strictPort",
		url: "http://127.0.0.1:3417/e2e/fixtures/hls.html",
		reuseExistingServer: false,
	},
});

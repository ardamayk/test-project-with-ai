import { defineConfig } from "vitest/config";

export default defineConfig({
	cacheDir: process.env.EARTHLY_VITE_CACHE
		? `${process.env.EARTHLY_VITE_CACHE}/ui-unit`
		: undefined,
	esbuild: {
		jsx: "automatic",
	},
	test: {
		coverage: {
			reportsDirectory: process.env.EARTHLY_RUN_DIR
				? `${process.env.EARTHLY_RUN_DIR}/ui-unit/coverage`
				: "coverage",
		},
		environment: "jsdom",
		setupFiles: ["./vitest.setup.ts"],
		include: ["src/**/*.test.{ts,tsx}"],
	},
});

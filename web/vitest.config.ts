import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
	cacheDir: process.env.EARTHLY_VITE_CACHE
		? `${process.env.EARTHLY_VITE_CACHE}/web-unit`
		: undefined,
	plugins: [react(), tailwindcss()],
	resolve: { tsconfigPaths: true },
	test: {
		coverage: {
			reportsDirectory: process.env.EARTHLY_RUN_DIR
				? `${process.env.EARTHLY_RUN_DIR}/web-unit/coverage`
				: "coverage",
		},
		environment: "jsdom",
		include: ["src/**/*.test.{ts,tsx}"],
	},
});

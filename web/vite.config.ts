import babel from "@rolldown/plugin-babel";
import tailwindcss from "@tailwindcss/vite";
import { devtools } from "@tanstack/devtools-vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import viteReact, { reactCompilerPreset } from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const LAZY_HLS_CHUNK_SIZE_WARNING_LIMIT_KB = 550;

const config = defineConfig({
	resolve: { tsconfigPaths: true },
	build: {
		chunkSizeWarningLimit: LAZY_HLS_CHUNK_SIZE_WARNING_LIMIT_KB,
		outDir: "dist",
	},
	server: {
		port: 3000,
		proxy: {
			"/api": {
				target: "http://localhost:8090",
				changeOrigin: true,
			},
			"/docs": {
				target: "http://localhost:8090",
				changeOrigin: true,
			},
		},
	},
	plugins: [
		devtools(),
		tailwindcss(),
		tanstackRouter({ target: "react", autoCodeSplitting: true }),
		viteReact(),
		babel({ presets: [reactCompilerPreset()] }),
	],
});

export default config;

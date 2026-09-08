import mdx from "@mdx-js/rollup";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
	cacheDir: process.env.EARTHLY_VITE_CACHE
		? `${process.env.EARTHLY_VITE_CACHE}/docs`
		: undefined,
	base: "/docs/",
	plugins: [{ enforce: "pre", ...mdx() }, react()],
	build: {
		outDir: process.env.EARTHLY_DOCS_DIST ?? "dist",
		emptyOutDir: true,
	},
});

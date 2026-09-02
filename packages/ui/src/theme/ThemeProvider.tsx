import type { ThemePreferences } from "@repo/api-client";
import { useEffect } from "react";

function resolveDarkMode(mode: ThemePreferences["mode"]): boolean {
	if (mode === "dark") return true;
	if (mode === "light") return false;
	return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

export function ThemeProvider({ theme }: { theme: ThemePreferences }) {
	useEffect(() => {
		const root = document.documentElement;
		root.dataset.themePreset = theme.preset;

		const apply = () => {
			const isDark = resolveDarkMode(theme.mode);
			root.classList.toggle("dark", isDark);
		};

		apply();

		if (theme.mode !== "system") return;

		const media = window.matchMedia("(prefers-color-scheme: dark)");
		const onChange = () => apply();
		media.addEventListener("change", onChange);
		return () => media.removeEventListener("change", onChange);
	}, [theme.mode, theme.preset]);

	return null;
}

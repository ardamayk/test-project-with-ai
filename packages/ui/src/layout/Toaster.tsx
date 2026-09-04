import type { ThemePreferences } from "@repo/api-client";
import { useEffect, useState } from "react";
import { Toaster as SonnerToaster } from "sonner";
import { useLayout } from "./LayoutProvider";

function resolveSonnerTheme(mode: ThemePreferences["mode"]): "light" | "dark" {
	if (mode === "dark") return "dark";
	if (mode === "light") return "light";
	if (typeof window.matchMedia !== "function") return "light";
	return window.matchMedia("(prefers-color-scheme: dark)").matches
		? "dark"
		: "light";
}

/**
 * Application-wide toast surface. Mounted once by AppShell so both the Web
 * and Desktop Clients raise toasts through the same `toast()` export; the
 * theme follows the user's Theme Preferences the way ThemeProvider does.
 */
export function Toaster() {
	const { preferences } = useLayout();
	const mode = preferences.theme.mode;
	const [theme, setTheme] = useState<"light" | "dark">(() =>
		resolveSonnerTheme(mode),
	);

	useEffect(() => {
		setTheme(resolveSonnerTheme(mode));
		if (mode !== "system" || typeof window.matchMedia !== "function") return;
		const media = window.matchMedia("(prefers-color-scheme: dark)");
		const onChange = () => setTheme(resolveSonnerTheme(mode));
		media.addEventListener("change", onChange);
		return () => media.removeEventListener("change", onChange);
	}, [mode]);

	return (
		<SonnerToaster
			theme={theme}
			position="bottom-right"
			closeButton
			toastOptions={{
				classNames: {
					toast:
						"group toast group-[.toaster]:bg-popover group-[.toaster]:text-popover-foreground group-[.toaster]:border-border group-[.toaster]:shadow-lg",
					description: "group-[.toast]:text-caption",
					actionButton:
						"group-[.toast]:bg-primary group-[.toast]:text-primary-foreground",
					cancelButton:
						"group-[.toast]:bg-muted group-[.toast]:text-muted-foreground",
				},
			}}
		/>
	);
}

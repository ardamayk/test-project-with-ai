import type {
	LayoutPreferences,
	ThemePreferences,
	UserPreferences,
} from "@repo/api-client";
import type { ComponentType } from "react";

export type WidgetDefinition = {
	id: string;
	title: string;
	component: ComponentType;
};

export type WidgetPlacement = "left" | "right" | "main";

export const defaultLayout: LayoutPreferences = {
	sidebarPosition: "left",
	panels: {
		left: ["now-playing"],
		right: ["discover"],
	},
	collapsed: { left: false, right: false },
	sizes: [22, 50, 28],
};

export const defaultTheme: ThemePreferences = {
	mode: "system",
	preset: "earthly",
};

export const defaultPreferences: UserPreferences = {
	theme: defaultTheme,
	layout: defaultLayout,
};

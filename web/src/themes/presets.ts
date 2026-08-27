import type { ThemePresetMeta } from "./registry";
import { THEME_PRESET_REGISTRY } from "./registry";

export type { ThemePalette, ThemePresetId, ThemePresetMeta } from "./registry";
export { THEME_PRESET_IDS, THEME_PRESET_REGISTRY } from "./registry";

export const themePresetOptions: ThemePresetMeta[] = THEME_PRESET_REGISTRY.map(
	(preset) => ({
		id: preset.id,
		label: preset.label,
		swatches: [...preset.colors],
	}),
);

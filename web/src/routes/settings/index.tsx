import type { ThemePreferences } from "@repo/api-client";
import { useLayout } from "@repo/ui";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Button } from "#/components/ui/button";
import { apiClient } from "#/lib/api";
import { cn } from "#/lib/utils";
import { themePresetOptions } from "#/themes/presets";

const themeModes: ThemePreferences["mode"][] = ["light", "dark", "system"];

export const Route = createFileRoute("/settings/")({
	component: SettingsPage,
});

function SettingsPage() {
	const { preferences, setPreferences } = useLayout();

	const health = useQuery({
		queryKey: ["health"],
		queryFn: () => apiClient.getHealth(),
	});

	const saveTheme = (theme: Partial<ThemePreferences>) => {
		const next = { ...preferences.theme, ...theme };
		setPreferences({ theme: next });
	};

	return (
		<div className="p-6">
			<header className="mb-6">
				<h1 className="font-semibold text-2xl">Settings</h1>
				<p className="text-foreground text-sm">
					Appearance · API {health.data?.status ?? "…"} v
					{health.data?.version ?? "…"}
				</p>
			</header>

			<section className="mb-8 flex flex-col gap-4">
				<h2 className="font-medium text-sm">Theme preset</h2>
				<div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
					{themePresetOptions.map((preset) => {
						const isActive = preferences.theme.preset === preset.id;

						return (
							<button
								key={preset.id}
								type="button"
								onClick={() => saveTheme({ preset: preset.id })}
								className={cn(
									"rounded-xl border p-3 text-left transition",
									isActive
										? "border-primary bg-primary/5 ring-2 ring-primary/30"
										: "border-border bg-card/40 hover:bg-muted/40",
								)}
							>
								<div className="mb-3 flex h-10 overflow-hidden rounded-lg border border-border/60">
									{preset.swatches.map((color) => (
										<span
											key={color}
											className="h-full flex-1"
											style={{ backgroundColor: color }}
										/>
									))}
								</div>
								<p className="font-medium text-heading text-sm">
									{preset.label}
								</p>
							</button>
						);
					})}
				</div>
			</section>

			<section className="flex flex-col gap-4">
				<h2 className="font-medium text-sm">Appearance</h2>
				<div className="flex flex-wrap gap-2">
					{themeModes.map((mode) => (
						<Button
							key={mode}
							type="button"
							size="sm"
							variant={
								preferences.theme.mode === mode ? "default" : "secondary"
							}
							onClick={() => saveTheme({ mode })}
						>
							{mode}
						</Button>
					))}
				</div>
			</section>
		</div>
	);
}

import type { ReactNode } from "react";
import { getNavPanel, getQueuePanel } from "../widgets/layout-utils";
import { useLayout } from "./LayoutProvider";
import { QueuePanel } from "./QueuePanel";
import { WidgetDndProvider, WidgetDock } from "./WidgetDock";

export function AppShell({
	children,
	sidebar,
	bottom,
}: {
	children?: ReactNode;
	sidebar?: ReactNode;
	bottom?: ReactNode;
}) {
	const { preferences } = useLayout();
	const { layout } = preferences;
	const navPanel = getNavPanel(layout.sidebarPosition);
	const queuePanel = getQueuePanel(layout.sidebarPosition);
	const navCollapsed = layout.collapsed[navPanel];
	const queueCollapsed = layout.collapsed[queuePanel];

	const navWidgetPanel = navPanel === "left" ? "left" : "right";
	const queueWidgetPanel = queuePanel === "left" ? "left" : "right";

	const navColumn = (
		<div className="flex h-full w-full flex-col overflow-hidden bg-sidebar">
			{sidebar}
			{!navCollapsed ? (
				<div className="min-h-0 flex-1 overflow-y-auto border-sidebar-border border-t">
					<WidgetDock panel={navWidgetPanel} />
				</div>
			) : null}
		</div>
	);
	const fixedNavColumn = (
		<aside className="h-full w-fit min-w-max max-w-[min(22rem,45vw)] shrink-0 overflow-hidden">
			{navColumn}
		</aside>
	);

	const queueColumn = (
		<div className="flex h-full w-full flex-col overflow-hidden bg-queue text-queue-foreground">
			<div className="min-h-0 flex-[2] overflow-hidden">
				<QueuePanel />
			</div>
			{!queueCollapsed ? (
				<div className="min-h-0 flex-1 overflow-y-auto border-border border-t">
					<WidgetDock panel={queueWidgetPanel} />
				</div>
			) : null}
		</div>
	);
	const fixedQueueColumn = (
		<aside
			data-queue-column
			className={`h-full w-80 shrink-0 overflow-hidden border-border ${
				queuePanel === "left" ? "border-r" : "border-l"
			}`}
		>
			{queueColumn}
		</aside>
	);

	return (
		<WidgetDndProvider>
			<div className="flex h-full min-h-0 w-full flex-1 flex-col overflow-hidden bg-background text-foreground">
				<div className="flex min-h-0 flex-1 overflow-hidden">
					{navPanel === "left" ? fixedNavColumn : null}
					{queuePanel === "left" && !queueCollapsed ? fixedQueueColumn : null}
					<main className="flex h-full min-w-0 flex-1 flex-col overflow-auto bg-background">
						{children}
					</main>
					{queuePanel === "right" && !queueCollapsed ? fixedQueueColumn : null}
					{navPanel === "right" ? fixedNavColumn : null}
				</div>
				{bottom ? (
					<div className="shrink-0 border-border border-t">{bottom}</div>
				) : null}
			</div>
		</WidgetDndProvider>
	);
}

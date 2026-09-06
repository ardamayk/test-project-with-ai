import type { ReactNode } from "react";
import { getNavPanel, getQueuePanel } from "../widgets/layout-utils";
import { useLayout } from "./LayoutProvider";
import { QueuePanel } from "./QueuePanel";
import { Toaster } from "./Toaster";
import { WidgetDndProvider, WidgetDock } from "./WidgetDock";

// Floating player bar: 72px bar + 16px bottom inset + 16px breathing room, so
// scrolled content never ends hidden under the bar.
const PLAYER_DOCK_CONTENT_PADDING = "pb-[104px]";
// The dock follows the page content column so the bar lines up with page
// headers and lists. Mirrors PAGE_CONTENT_PADDING_CLASS and
// PAGE_CONTENT_WIDTH_CLASS in web/src/lib/page-layout-classes.ts.
const PLAYER_DOCK_PADDING_CLASS = "px-6 md:px-8";
const PLAYER_DOCK_COLUMN_CLASS = "mx-auto w-full min-[1801px]:max-w-[1476px]";

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
					<div className="relative flex h-full min-w-0 flex-1 flex-col overflow-hidden">
						<main
							className={`flex h-full min-w-0 flex-1 flex-col overflow-auto bg-background ${
								bottom ? PLAYER_DOCK_CONTENT_PADDING : ""
							}`}
						>
							{children}
						</main>
						{bottom ? (
							<div
								data-player-dock
								className={`pointer-events-none absolute inset-x-0 bottom-4 z-30 ${PLAYER_DOCK_PADDING_CLASS}`}
							>
								<div
									data-player-dock-column
									className={PLAYER_DOCK_COLUMN_CLASS}
								>
									<div className="pointer-events-auto">{bottom}</div>
								</div>
							</div>
						) : null}
					</div>
					{queuePanel === "right" && !queueCollapsed ? fixedQueueColumn : null}
					{navPanel === "right" ? fixedNavColumn : null}
				</div>
				<Toaster />
			</div>
		</WidgetDndProvider>
	);
}

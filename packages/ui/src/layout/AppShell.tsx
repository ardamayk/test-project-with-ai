import type { ReactNode } from "react";
import { getQueuePanel } from "../widgets/layout-utils";
import { useLayout } from "./LayoutProvider";
import { QueueDrawer } from "./QueueDrawer";
import {
	QUEUE_DRAWER_CONTENT_CLEARANCE,
	SHELL_INSET_CLASS,
} from "./shell-metrics";
import { Toaster } from "./Toaster";
import { TopNav } from "./TopNav";
import { WidgetDndProvider } from "./WidgetDock";

export { PLAYER_BAR_HEIGHT_PX, PLAYER_BAR_INSET_PX } from "./shell-metrics";

// Floating player bar: 80px bar + 16px bottom inset + 8px breathing room, so
// scrolled content never ends hidden under the bar.
const PLAYER_DOCK_CONTENT_PADDING = "pb-[104px]";

/**
 * App Shell: Top Nav across the top, the page below it, the Player Bar
 * floating over the page's bottom edge, and the Queue Drawer sliding up on
 * the right. Nav, page and bar share one horizontal inset; while the drawer
 * is open the page gives way to it and the nav and bar stay put.
 */
export function AppShell({
	children,
	bottom,
	onSearch,
}: {
	children?: ReactNode;
	bottom?: ReactNode;
	/** Opens the host's library search; shown as "Search" in the Top Nav. */
	onSearch?: () => void;
}) {
	const { preferences } = useLayout();
	const queuePanel = getQueuePanel(preferences.layout.sidebarPosition);
	const queueOpen = !preferences.layout.collapsed[queuePanel];

	return (
		<WidgetDndProvider>
			<div
				data-app-shell
				className="relative flex h-full min-h-0 w-full flex-1 flex-col overflow-hidden bg-background text-foreground"
			>
				<TopNav onSearch={onSearch} />
				<div className="relative flex h-full min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
					<main
						data-queue-open={queueOpen ? "" : undefined}
						className={`flex h-full min-w-0 flex-1 flex-col overflow-auto bg-background transition-[padding] duration-300 ease-out ${
							bottom ? PLAYER_DOCK_CONTENT_PADDING : ""
						}`}
						style={{
							paddingRight: queueOpen ? QUEUE_DRAWER_CONTENT_CLEARANCE : 0,
						}}
					>
						{children}
					</main>
					{bottom ? (
						<div
							data-player-scrim
							aria-hidden
							className="pointer-events-none absolute inset-x-0 bottom-0 z-20 h-36 bg-gradient-to-t from-background via-background/75 to-transparent"
						/>
					) : null}
					{bottom ? (
						<div
							data-player-dock
							className={`pointer-events-none absolute inset-x-0 bottom-4 z-30 ${SHELL_INSET_CLASS}`}
						>
							<div data-player-dock-column className="w-full">
								<div className="pointer-events-auto">{bottom}</div>
							</div>
						</div>
					) : null}
				</div>
				<QueueDrawer abovePlayerBar={Boolean(bottom)} />
				<Toaster />
			</div>
		</WidgetDndProvider>
	);
}

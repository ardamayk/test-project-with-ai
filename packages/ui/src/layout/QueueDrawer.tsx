import { X } from "lucide-react";
import { cn } from "../lib/utils";
import { getQueuePanel } from "../widgets/layout-utils";
import { useLayout } from "./LayoutProvider";
import { QueuePanel } from "./QueuePanel";
import {
	QUEUE_DRAWER_BOTTOM,
	QUEUE_DRAWER_BOTTOM_ABOVE_PLAYER,
	QUEUE_DRAWER_TOP,
	QUEUE_DRAWER_WIDTH,
} from "./shell-metrics";
import { WidgetDock } from "./WidgetDock";

/**
 * The Queue as a card that slides up from the bottom edge and parks between
 * the Top Nav and the Player Bar, its right edge on the page inset line. Open
 * state is the Queue panel's `collapsed` preference, so the Player Bar's queue
 * button and this card's close button drive the same flag.
 */
export function QueueDrawer({ abovePlayerBar }: { abovePlayerBar: boolean }) {
	const { preferences, togglePanel } = useLayout();
	const queuePanel = getQueuePanel(preferences.layout.sidebarPosition);
	const open = !preferences.layout.collapsed[queuePanel];

	return (
		<aside
			data-queue-drawer
			data-state={open ? "open" : "closed"}
			aria-label="Queue"
			aria-hidden={!open}
			inert={!open}
			className={cn(
				"absolute z-40 overflow-hidden rounded-2xl border border-border shadow-[0_-16px_48px_-12px_var(--player-shadow),0_14px_40px_-8px_var(--player-shadow)] transition-[translate,visibility] duration-300 ease-out",
				// Closed: slides below the viewport, then goes fully invisible so
				// it can never peek out under the Player Bar, whatever the host
				// window's geometry does to the shell.
				open
					? "visible translate-y-0"
					: "invisible translate-y-[calc(100%+8rem)]",
			)}
			style={{
				width: QUEUE_DRAWER_WIDTH,
				right: "var(--shell-inset, 2rem)",
				top: QUEUE_DRAWER_TOP,
				bottom: abovePlayerBar
					? QUEUE_DRAWER_BOTTOM_ABOVE_PLAYER
					: QUEUE_DRAWER_BOTTOM,
			}}
		>
			<div className="relative flex h-full w-full flex-col overflow-hidden bg-queue text-queue-foreground">
				<button
					type="button"
					onClick={() => togglePanel(queuePanel)}
					aria-label="Hide queue"
					title="Hide queue"
					className="absolute top-2 right-2 z-10 inline-flex size-7 items-center justify-center rounded-md text-caption transition hover:bg-muted hover:text-foreground"
				>
					<X className="size-4" />
				</button>
				<div className="min-h-0 flex-[2] overflow-hidden">
					<QueuePanel embedded />
				</div>
				<div className="min-h-0 flex-1 overflow-y-auto border-border border-t">
					<WidgetDock panel={queuePanel} />
				</div>
			</div>
		</aside>
	);
}

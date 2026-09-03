import { Link } from "@tanstack/react-router";
import {
	ListMusic,
	Music2,
	Radio,
	Settings,
	SquareLibrary,
	Tags,
	Users,
} from "lucide-react";
import { cn } from "../lib/utils";
import { getNavPanel } from "../widgets/layout-utils";
import { AppBrand } from "./AppBrand";
import { useLayout } from "./LayoutProvider";
import { PanelCollapseButton } from "./PanelCollapseButton";

const libraryNav = [
	{ to: "/library/albums", label: "Albums", icon: SquareLibrary },
	{ to: "/library/artists", label: "Artists", icon: Users },
	{ to: "/library/genres", label: "Genres", icon: Tags },
	{ to: "/radio", label: "Radio Stations", icon: Radio },
	{ to: "/library/tracks", label: "Tracks", icon: Music2 },
	{ to: "/playlists", label: "Playlists", icon: ListMusic },
] as const;

const footerNav = [
	{ to: "/settings", label: "Settings", icon: Settings },
] as const;

function NavLink({
	to,
	label,
	icon: Icon,
	compact = false,
}: {
	to: string;
	label: string;
	icon: typeof ListMusic;
	compact?: boolean;
}) {
	if (compact) {
		return (
			<Link
				to={to}
				title={label}
				className="flex size-9 items-center justify-center rounded rounded-l-none border-l-2 border-transparent text-sidebar-foreground transition hover:bg-[var(--shell-active)] hover:text-[var(--shell-active-foreground)] [&_svg]:text-current [&.active]:border-[var(--shell-active-foreground)] [&.active]:bg-[var(--shell-active)] [&.active]:text-[var(--shell-active-foreground)]"
			>
				<Icon className="size-4 shrink-0" />
			</Link>
		);
	}

	return (
		<Link
			to={to}
			className="flex h-10 items-center gap-4 whitespace-nowrap rounded rounded-l-none border-l-2 border-transparent px-4 py-2 text-base text-sidebar-foreground transition hover:bg-[var(--shell-active)] hover:text-[var(--shell-active-foreground)] [&_svg]:text-current [&.active]:border-[var(--shell-active-foreground)] [&.active]:bg-[var(--shell-active)] [&.active]:font-bold [&.active]:text-[var(--shell-active-foreground)]"
		>
			<Icon className="size-4 shrink-0" />
			{label}
		</Link>
	);
}

export function SidebarNav() {
	const { preferences, togglePanel } = useLayout();
	const panelSide = getNavPanel(preferences.layout.sidebarPosition);
	const isCollapsed = preferences.layout.collapsed[panelSide];

	if (isCollapsed) {
		return (
			<div className="flex h-full flex-col items-center bg-sidebar text-sidebar-foreground">
				<div className="flex w-full justify-center px-1 pt-2">
					<PanelCollapseButton
						edge={panelSide}
						collapsed
						onToggle={() => togglePanel(panelSide)}
					/>
				</div>
				<AppBrand compact />
				<nav className="flex flex-1 flex-col items-center gap-2 overflow-y-auto py-2">
					{libraryNav.map((item) => (
						<NavLink key={item.to} {...item} compact />
					))}
					<div className="my-2 w-6 border-sidebar-border border-t" />
					{footerNav.map((item) => (
						<NavLink key={item.to} {...item} compact />
					))}
				</nav>
			</div>
		);
	}

	return (
		<div className="flex h-full w-[260px] flex-col bg-sidebar text-sidebar-foreground">
			<div
				className={cn(
					"flex items-start gap-1 px-6 pt-6 pb-8",
					panelSide === "right" && "flex-row-reverse",
				)}
			>
				<AppBrand />
				<PanelCollapseButton
					edge={panelSide}
					collapsed={false}
					onToggle={() => togglePanel(panelSide)}
					className="mt-1"
				/>
			</div>
			<nav className="flex flex-1 flex-col gap-1 overflow-y-auto px-4">
				{libraryNav.map((item) => (
					<NavLink key={item.to} {...item} />
				))}
			</nav>
			<nav className="pb-20 px-4">
				{footerNav.map((item) => (
					<NavLink key={item.to} {...item} />
				))}
			</nav>
		</div>
	);
}

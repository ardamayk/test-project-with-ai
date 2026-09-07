import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
	AppShell,
	defaultPreferences,
	LayoutProvider,
	PlaybackProvider,
	TopNav,
} from "../index";
import { InMemoryPlaybackEngine } from "../playback/testing/InMemoryPlaybackEngine";

vi.mock("@tanstack/react-router", () => ({
	Link: ({
		children,
		className,
		title,
		to,
	}: {
		children: ReactNode;
		className?: string;
		title?: string;
		to: string;
	}) => (
		<a className={className} href={to} title={title}>
			{children}
		</a>
	),
}));

const mockPlaybackApi = {
	getQueue: async () => ({ items: [], revision: "0" }),
	replaceQueue: async () => ({ items: [], revision: "1" }),
	reorderQueue: async () => ({ items: [], revision: "1" }),
	appendQueueItem: async () => ({ items: [], revision: "1" }),
	removeQueueItem: async () => ({ items: [], revision: "1" }),
	clearQueue: async () => ({ items: [], revision: "1" }),
	getStreamUrl: (id: string) => `/stream/${id}`,
	getAlbumCoverUrl: (id: string) => `/cover/${id}`,
	getRadioStationStreamUrl: (id: string) => `/radio/${id}`,
	getRadioCatalogPreviewStreamUrl: (id: string) => `/radio/preview/${id}`,
	getRadioNowPlaying: async () => ({}),
	listPlaylists: async () => ({ items: [], total: 0 }),
	getPlaylist: async (playlistId: string) => ({
		id: playlistId,
		name: "Playlist",
		isDefault: false,
		trackCount: 0,
		tracks: [],
	}),
	createPlaylist: async (name: string) => ({
		id: "playlist-1",
		name,
		isDefault: false,
		trackCount: 0,
	}),
	addPlaylistTrack: async () => ({
		id: "playlist-1",
		name: "Playlist",
		isDefault: false,
		trackCount: 1,
		tracks: [],
	}),
	removePlaylistTrack: async () => ({
		id: "playlist-1",
		name: "Playlist",
		isDefault: false,
		trackCount: 0,
		tracks: [],
	}),
};

const collapsedQueuePreferences = {
	...defaultPreferences,
	layout: {
		...defaultPreferences.layout,
		collapsed: { left: false, right: true },
	},
};

function renderShell(
	ui: ReactNode,
	preferences: typeof defaultPreferences = defaultPreferences,
) {
	return render(
		<LayoutProvider initialPreferences={preferences}>
			<PlaybackProvider
				api={mockPlaybackApi}
				engine={new InMemoryPlaybackEngine()}
			>
				{ui}
			</PlaybackProvider>
		</LayoutProvider>,
	);
}

describe("AppShell", () => {
	afterEach(cleanup);

	it("renders the top nav, main content, queue and widgets", () => {
		renderShell(
			<AppShell>
				<div>Main content</div>
			</AppShell>,
		);
		expect(screen.getByText("Main content")).toBeTruthy();
		expect(screen.getByRole("link", { name: "Albums" })).toBeTruthy();
		expect(screen.getByRole("link", { name: "Settings" })).toBeTruthy();
		expect(screen.getByText("Queue")).toBeTruthy();
		expect(screen.getByText("Discover (coming soon)")).toBeTruthy();
	});

	it("has no side columns: the nav is a top bar and the queue a drawer", () => {
		const { container } = renderShell(
			<AppShell>
				<div>Main content</div>
			</AppShell>,
		);
		expect(container.querySelector("header")).toBeTruthy();
		expect(container.querySelector("[data-queue-column]")).toBeNull();
		expect(container.querySelector("[data-queue-drawer]")).toBeTruthy();
		expect(
			container.querySelectorAll('[data-slot="resizable-handle"]'),
		).toHaveLength(0);
	});

	it("aligns the floating player dock with the shared page inset", () => {
		const { container } = renderShell(
			<AppShell bottom={<div>Player</div>}>
				<div>Main content</div>
			</AppShell>,
		);
		const scrim = container.querySelector("[data-player-scrim]");
		expect(scrim?.className).toContain("bg-gradient-to-t");
		expect(scrim?.className).toContain("pointer-events-none");
		const dock = container.querySelector("[data-player-dock]");
		const column = container.querySelector("[data-player-dock-column]");
		expect(dock?.className).toContain("inset-x-0");
		// Mirrors PAGE_CONTENT_PADDING_CLASS in web: the same --shell-inset.
		expect(dock?.className).toContain("px-[var(--shell-inset,2rem)]");
		expect(column?.className).toContain("w-full");
		expect(column?.className).not.toContain("max-w-");
	});

	it("opens the queue drawer and makes room for it in the page", () => {
		const { container } = renderShell(
			<AppShell bottom={<div>Player</div>}>
				<div>Main content</div>
			</AppShell>,
		);
		const drawer = container.querySelector(
			"[data-queue-drawer]",
		) as HTMLElement;
		expect(drawer.dataset.state).toBe("open");
		expect(drawer.getAttribute("aria-hidden")).toBe("false");
		expect(drawer.className).toContain("translate-y-0");
		expect(drawer.className).not.toContain("invisible");
		expect(drawer.style.right).toBe("var(--shell-inset, 2rem)");
		// Parks above the Player Bar rather than touching it.
		expect(drawer.style.bottom).toContain("80px");
		const main = container.querySelector("main") as HTMLElement;
		expect(main.hasAttribute("data-queue-open")).toBe(true);
		expect(main.style.paddingRight).toBe("");
		expect(main.className).toContain(
			"lg:data-[queue-open]:pr-[var(--queue-drawer-clearance)]",
		);
	});

	it("keeps the drawer off screen and inert when the queue is collapsed", () => {
		const { container } = renderShell(
			<AppShell>
				<div>Main content</div>
			</AppShell>,
			collapsedQueuePreferences,
		);
		const drawer = container.querySelector(
			"[data-queue-drawer]",
		) as HTMLElement;
		expect(drawer.dataset.state).toBe("closed");
		expect(drawer.getAttribute("aria-hidden")).toBe("true");
		expect(drawer.hasAttribute("inert")).toBe(true);
		expect(drawer.className).toContain("translate-y-[calc(100%+8rem)]");
		expect(drawer.className).toContain("invisible");
		const main = container.querySelector("main") as HTMLElement;
		expect(main.hasAttribute("data-queue-open")).toBe(false);
		expect(main.style.paddingRight).toBe("");
		expect(screen.getByText("Main content")).toBeTruthy();
	});

	it("closes the drawer from its own close button", () => {
		const { container } = renderShell(
			<AppShell>
				<div>Main content</div>
			</AppShell>,
		);
		fireEvent.click(screen.getByRole("button", { name: "Hide queue" }));
		const drawer = container.querySelector(
			"[data-queue-drawer]",
		) as HTMLElement;
		expect(drawer.dataset.state).toBe("closed");
	});

	it("keeps widget content from changing the drawer width", () => {
		const { container } = renderShell(
			<AppShell>
				<div>Main content</div>
			</AppShell>,
		);
		const widgetDock = container.querySelector("[data-widget-dock]");
		expect(widgetDock).toBeTruthy();
		const className = (widgetDock as HTMLElement | null)?.className ?? "";
		expect(className).toContain("[contain:inline-size]");
		expect(className).toContain("min-w-0");
	});

	it("renders the library sections, Search and Settings in the top nav", () => {
		const onSearch = vi.fn();
		render(<TopNav onSearch={onSearch} />);
		for (const label of [
			"Albums",
			"Artists",
			"Genres",
			"Radio",
			"Tracks",
			"Playlists",
		]) {
			expect(screen.getByRole("link", { name: label })).toBeTruthy();
		}
		fireEvent.click(screen.getByRole("button", { name: "Search" }));
		expect(onSearch).toHaveBeenCalledTimes(1);
		const radioLink = screen.getByRole("link", { name: "Radio" });
		expect(radioLink.className).toContain(
			"[&.active]:bg-[var(--shell-active)]",
		);
		expect(screen.getByRole("link", { name: "Settings" })).toBeTruthy();
		expect(screen.queryByText("Premium Account")).toBeNull();
	});
});

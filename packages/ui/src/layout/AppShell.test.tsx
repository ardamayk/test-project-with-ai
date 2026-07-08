import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	AppShell,
	defaultPreferences,
	LayoutProvider,
	PlaybackProvider,
	SidebarNav,
} from "../index";

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
	getQueue: async () => ({ items: [] }),
	replaceQueue: async () => ({ items: [] }),
	appendQueueItem: async () => ({ items: [] }),
	removeQueueItem: async () => ({ items: [] }),
	clearQueue: async () => ({ items: [] }),
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

class AudioMock extends EventTarget {
	volume = 1;
	pause = vi.fn();
	play = vi.fn(async () => {});
	removeAttribute = vi.fn();
}

describe("AppShell", () => {
	const originalAudio = globalThis.Audio;

	beforeEach(() => {
		globalThis.Audio = AudioMock as unknown as typeof Audio;
	});

	afterEach(() => {
		cleanup();
		globalThis.Audio = originalAudio;
	});

	it("renders main content and widgets", () => {
		render(
			<LayoutProvider initialPreferences={defaultPreferences}>
				<PlaybackProvider api={mockPlaybackApi}>
					<AppShell>
						<div>Main content</div>
					</AppShell>
				</PlaybackProvider>
			</LayoutProvider>,
		);
		expect(screen.getByText("Main content")).toBeTruthy();
		expect(screen.getByText("Nothing playing")).toBeTruthy();
		expect(screen.getByText("Queue")).toBeTruthy();
	});

	it("does not render a resize handle for the primary nav column", () => {
		const { container } = render(
			<LayoutProvider initialPreferences={defaultPreferences}>
				<PlaybackProvider api={mockPlaybackApi}>
					<AppShell>
						<div>Main content</div>
					</AppShell>
				</PlaybackProvider>
			</LayoutProvider>,
		);

		expect(
			container.querySelectorAll('[data-slot="resizable-handle"]'),
		).toHaveLength(1);
	});

	it("sizes the primary nav column to its content instead of saved panel width", () => {
		const { container } = render(
			<LayoutProvider initialPreferences={defaultPreferences}>
				<PlaybackProvider api={mockPlaybackApi}>
					<AppShell>
						<div>Main content</div>
					</AppShell>
				</PlaybackProvider>
			</LayoutProvider>,
		);

		const navColumn = container.querySelector("aside");
		expect(navColumn?.className).toContain("w-fit");
		expect((navColumn as HTMLElement | null)?.style.width).toBe("");
	});

	it("keeps widget content from changing the primary nav column width", () => {
		const { container } = render(
			<LayoutProvider initialPreferences={defaultPreferences}>
				<PlaybackProvider api={mockPlaybackApi}>
					<AppShell>
						<div>Main content</div>
					</AppShell>
				</PlaybackProvider>
			</LayoutProvider>,
		);

		const widgetDock = container.querySelector("[data-widget-dock]");
		expect(widgetDock).toBeTruthy();
		const className = (widgetDock as HTMLElement | null)?.className ?? "";
		expect(className).toContain("[contain:inline-size]");
		expect(className).toContain("min-w-0");
	});

	it("hides the queue column when the queue panel is collapsed", () => {
		const collapsedQueuePreferences = {
			...defaultPreferences,
			layout: {
				...defaultPreferences.layout,
				collapsed: { left: false, right: true },
			},
		};

		render(
			<LayoutProvider initialPreferences={collapsedQueuePreferences}>
				<PlaybackProvider api={mockPlaybackApi}>
					<AppShell>
						<div>Main content</div>
					</AppShell>
				</PlaybackProvider>
			</LayoutProvider>,
		);

		expect(screen.queryByText("Queue")).toBeNull();
		expect(screen.queryByTitle("Queue")).toBeNull();
		expect(screen.getByText("Main content")).toBeTruthy();
	});

	it("renders the Figma sidebar navigation treatment", () => {
		render(
			<LayoutProvider initialPreferences={defaultPreferences}>
				<SidebarNav />
			</LayoutProvider>,
		);

		const radioLink = screen.getByRole("link", { name: "Radio Stations" });
		expect(screen.getByText("Premium Account")).toBeTruthy();
		expect(screen.queryByText("Help (soon)")).toBeNull();
		expect(radioLink.className).toContain(
			"[&.active]:bg-[var(--shell-active)]",
		);
		expect(radioLink.className).toContain(
			"[&.active]:text-[var(--shell-active-foreground)]",
		);
	});
});

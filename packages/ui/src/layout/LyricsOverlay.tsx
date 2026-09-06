import type { Track } from "@repo/api-client";
import { ChevronDown } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { AlbumArt } from "./AlbumArt";
import { QueuePanel } from "./QueuePanel";

/**
 * Full-screen "Lyrics" view that slides up over the app: lyrics on the left,
 * the shared Queue on the right. Lyrics are not stored by the Music Server
 * yet, so the left side shows the Track and an empty state until they are.
 */
type LyricsState =
	| { status: "loading" }
	| { status: "loaded"; lyrics: string }
	| { status: "failed" };

export function LyricsOverlay({
	track,
	coverUrl,
	loadLyrics,
	onClose,
}: {
	track: Track;
	coverUrl: string | null;
	/** Resolves the stored lyrics; null when the host has no lyrics endpoint. */
	loadLyrics?: (trackId: string) => Promise<{ lyrics: string } | null>;
	onClose: () => void;
}) {
	const [state, setState] = useState<LyricsState>({ status: "loading" });

	useEffect(() => {
		let cancelled = false;
		setState({ status: "loading" });
		if (!loadLyrics) {
			setState({ status: "loaded", lyrics: "" });
			return undefined;
		}
		loadLyrics(track.id)
			.then((result) => {
				if (!cancelled)
					setState({ status: "loaded", lyrics: result?.lyrics ?? "" });
			})
			.catch(() => {
				if (!cancelled) setState({ status: "failed" });
			});
		return () => {
			cancelled = true;
		};
	}, [loadLyrics, track.id]);

	useEffect(() => {
		const closeOnEscape = (event: KeyboardEvent) => {
			if (event.key === "Escape") onClose();
		};
		document.addEventListener("keydown", closeOnEscape);
		return () => document.removeEventListener("keydown", closeOnEscape);
	}, [onClose]);

	if (typeof document === "undefined") return null;

	return createPortal(
		<div
			role="dialog"
			aria-modal="true"
			aria-label="Lyrics"
			className="lyrics-view-enter fixed inset-0 z-[60] flex flex-col bg-background text-foreground"
		>
			<header className="flex shrink-0 items-center justify-between gap-4 border-border border-b px-6 py-3">
				<div className="flex min-w-0 items-center gap-3">
					<AlbumArt
						coverUrl={coverUrl}
						title={track.title}
						className="size-12 shrink-0 rounded-md text-sm"
					/>
					<div className="min-w-0">
						<p className="truncate font-semibold text-heading text-sm">
							{track.title}
						</p>
						<p className="truncate text-caption text-xs">
							{track.artistName}
							{track.albumTitle ? ` · ${track.albumTitle}` : ""}
						</p>
					</div>
				</div>
				<button
					type="button"
					className="inline-flex size-9 items-center justify-center rounded-full text-foreground hover:bg-muted"
					aria-label="Close lyrics"
					onClick={onClose}
				>
					<ChevronDown className="size-5" />
				</button>
			</header>
			<div className="flex min-h-0 flex-1">
				<section
					aria-label="Lyrics text"
					className="flex min-w-0 flex-1 flex-col overflow-y-auto px-8 py-10 [-ms-overflow-style:none] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
				>
					<LyricsBody state={state} />
				</section>
				<aside className="hidden w-[22rem] shrink-0 border-border border-l bg-queue text-queue-foreground md:block">
					<QueuePanel embedded />
				</aside>
			</div>
		</div>,
		document.body,
	);
}

function LyricsBody({ state }: { state: LyricsState }): ReactNode {
	if (state.status === "loading") {
		return (
			<p className="m-auto text-caption text-sm" aria-live="polite">
				Loading lyrics…
			</p>
		);
	}
	if (state.status === "failed") {
		return (
			<div className="m-auto max-w-md text-center" role="alert">
				<p className="font-semibold text-heading text-lg">
					Lyrics could not be loaded
				</p>
				<p className="mt-2 text-caption text-sm">
					The Music Server did not answer. Try again in a moment.
				</p>
			</div>
		);
	}
	const lines = state.lyrics
		.split(/\r?\n/)
		.map((line) => line.trimEnd())
		.filter((line, index, all) => line !== "" || all[index - 1] !== "");
	if (lines.every((line) => line === "")) {
		return (
			<div className="m-auto max-w-md text-center">
				<p className="font-semibold text-heading text-lg">No lyrics yet</p>
				<p className="mt-2 text-caption text-sm">
					This Track has no lyrics stored on your Music Server. Tag the file
					with lyrics (Picard writes them) and import it again.
				</p>
			</div>
		);
	}
	return (
		<div className="mx-auto w-full max-w-2xl">
			{lines.map((line, index) => (
				<p
					// biome-ignore lint/suspicious/noArrayIndexKey: lyric lines have no identity
					key={index}
					className="min-h-[1.5em] font-semibold text-2xl text-heading leading-relaxed"
				>
					{line}
				</p>
			))}
		</div>
	);
}

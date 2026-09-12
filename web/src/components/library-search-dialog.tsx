import type { LibrarySearchResult } from "@repo/api-client";
import { usePlayback } from "@repo/ui";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Disc3, ListMusic, Music2, Search, Tags, Users } from "lucide-react";
import { Dialog as DialogPrimitive } from "radix-ui";
import {
	useCallback,
	useEffect,
	useId,
	useMemo,
	useRef,
	useState,
} from "react";
import { useDebouncedValue } from "#/hooks/use-debounced-value";
import { useReturnFocus } from "#/hooks/use-return-focus";
import { apiClient } from "#/lib/api";
import { getTrackArtistName } from "#/lib/library-display";
import { libraryQueryKeys } from "#/lib/library-query-keys";
import { cn } from "#/lib/utils";

const RESULTS_PER_GROUP = 5;
const SEARCH_DEBOUNCE_MS = 200;

type SearchResult = {
	/** Unique across groups; also the option's DOM id suffix. */
	key: string;
	title: string;
	subtitle?: string;
	action: () => void;
};

type SearchGroup = {
	label: string;
	icon: typeof Music2;
	results: SearchResult[];
};

/**
 * Searches every library surface at once: Tracks, Albums, Artists, Genres and
 * Playlists. Results are grouped under "From Your Library"; choosing a track
 * plays it, anything else opens its page.
 */
export function LibrarySearchDialog({
	open,
	onOpenChange,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const [portalContainer, setPortalContainer] = useState<HTMLElement | null>(
		null,
	);
	useEffect(() => {
		setPortalContainer(
			document.querySelector<HTMLElement>("[data-app-shell] main"),
		);
	}, []);
	const [query, setQuery] = useState("");
	const [activeKey, setActiveKey] = useState<string | null>(null);
	const debouncedQuery = useDebouncedValue(query.trim(), SEARCH_DEBOUNCE_MS);
	const hasQuery = /[\p{L}\p{N}]/u.test(query);
	const isDebouncing = query.trim() !== debouncedQuery;
	const canShowResults = open && hasQuery && !isDebouncing;
	const inputRef = useRef<HTMLInputElement>(null);
	const listboxRef = useRef<HTMLDivElement>(null);
	const returnFocus = useReturnFocus();
	const navigate = useNavigate();
	const { playTrack } = usePlayback();
	const listboxId = useId();
	const measureResults = useCallback((content: HTMLDivElement | null) => {
		if (!content) return;
		const card = content.closest<HTMLElement>(".library-search-card");
		const header = inputRef.current?.parentElement;
		if (!card || !header) return;
		const measure = () => {
			const style = getComputedStyle(card);
			const border =
				Number.parseFloat(style.borderTopWidth) +
				Number.parseFloat(style.borderBottomWidth);
			card.style.height = `${Math.min(content.offsetHeight + header.offsetHeight + border, Number.parseFloat(style.maxHeight))}px`;
		};
		measure();
		// ponytail: measure natural content for WebKit; use CSS auto-size transitions once supported there.
		if (typeof ResizeObserver === "undefined") return;
		const observer = new ResizeObserver(measure);
		observer.observe(content);
		observer.observe(header);
		window.addEventListener("resize", measure);
		return () => {
			observer.disconnect();
			window.removeEventListener("resize", measure);
		};
	}, []);

	useLibrarySearchShortcut(open, onOpenChange);

	// Reset for the next opening; keep the last query while closed only
	// long enough for the close animation.
	useEffect(() => {
		if (!open) {
			setQuery("");
			setActiveKey(null);
		}
	}, [open]);

	const search = useQuery({
		queryKey: libraryQueryKeys.search(debouncedQuery),
		queryFn: () => apiClient.searchLibrary(debouncedQuery),
		enabled: canShowResults,
		retry: false,
	});

	const close = useCallback(() => onOpenChange(false), [onOpenChange]);

	const groups = useMemo<SearchGroup[]>(() => {
		if (!canShowResults || !search.data) return [];
		const toResult = (
			result: LibrarySearchResult,
			group: string,
		): SearchResult => ({
			key: `${group}:${result.type}:${result.id}`,
			title: result.name,
			subtitle: [
				getTrackArtistName({ artists: result.artists ?? [], artistName: "" }),
				result.album?.name,
			]
				.filter(Boolean)
				.join(" · "),
			action: () => {
				switch (result.type) {
					case "track":
						void playTrack(result.id);
						break;
					case "album":
						void navigate({
							to: "/library/$albumId",
							params: { albumId: result.id },
						});
						break;
					case "artist":
						void navigate({
							to: "/library/tracks",
							search: { artistId: result.id },
						});
						break;
					case "genre":
						void navigate({
							to: "/library/genres/$genre",
							params: { genre: result.name },
						});
						break;
					case "playlist":
						void navigate({
							to: "/playlists/$playlistId",
							params: { playlistId: result.id },
						});
						break;
				}
				close();
			},
		});
		const data = search.data;
		const best = data.bestMatch;
		const categories = [
			{ label: "Track", icon: Music2, results: data.tracks },
			{ label: "Album", icon: Disc3, results: data.albums },
			{ label: "Artist", icon: Users, results: data.artists },
			{ label: "Genre", icon: Tags, results: data.genres },
			{ label: "Playlist", icon: ListMusic, results: data.playlists },
		];
		return [
			...(best
				? [
						{
							label: "Best Match",
							icon: Search,
							results: [toResult(best, "best")],
						},
					]
				: []),
			...categories.map((group) => ({
				...group,
				results: group.results
					.slice(0, RESULTS_PER_GROUP)
					.map((result) => toResult(result, group.label.toLowerCase())),
			})),
		].filter((group) => group.results.length > 0);
	}, [canShowResults, search.data, navigate, playTrack, close]);

	const flatResults = useMemo(
		() => groups.flatMap((group) => group.results),
		[groups],
	);
	const activeResult =
		flatResults.find((result) => result.key === activeKey) ?? flatResults[0];
	const failed = canShowResults && search.isError;
	const isSearching = hasQuery && (isDebouncing || search.isFetching);
	const showEmpty =
		canShowResults && !isSearching && !failed && flatResults.length === 0;

	const scrollActiveResult = useCallback(() => {
		const listbox = listboxRef.current;
		if (!listbox || !activeResult) return;
		const option = document.getElementById(
			optionId(listboxId, activeResult.key),
		);
		if (!option || !listbox.contains(option)) return;
		const viewport = listbox.getBoundingClientRect();
		const row = option.getBoundingClientRect();
		if (row.top < viewport.top) {
			listbox.scrollTop += row.top - viewport.top;
		} else if (row.bottom > viewport.bottom) {
			listbox.scrollTop += row.bottom - viewport.bottom;
		}
	}, [activeResult, listboxId]);
	useEffect(scrollActiveResult, [scrollActiveResult]);

	const onInputKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
		if (flatResults.length === 0) return;
		if (event.key === "ArrowDown") {
			event.preventDefault();
			setActiveKey(
				flatResults[
					(flatResults.indexOf(activeResult) + 1) % flatResults.length
				].key,
			);
		} else if (event.key === "ArrowUp") {
			event.preventDefault();
			setActiveKey(
				flatResults[
					(flatResults.indexOf(activeResult) - 1 + flatResults.length) %
						flatResults.length
				].key,
			);
		} else if (event.key === "Enter") {
			event.preventDefault();
			activeResult?.action();
		}
	};

	return (
		<DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
			<DialogPrimitive.Portal container={portalContainer}>
				{/* Transparent: the page stays as it is behind the search card. */}
				<DialogPrimitive.Overlay className="fixed inset-0 z-50" />
				<DialogPrimitive.Content
					data-testid="library-search-dialog"
					onTransitionEnd={(event) => {
						if (
							event.target === event.currentTarget &&
							event.propertyName === "height"
						)
							scrollActiveResult();
					}}
					onOpenAutoFocus={(event) => {
						returnFocus.capture();
						event.preventDefault();
						inputRef.current?.focus();
					}}
					onCloseAutoFocus={returnFocus.restore}
					aria-describedby={undefined}
					className="library-search-card fixed top-1/2 z-50 flex max-h-[70vh] -translate-x-1/2 -translate-y-1/2 flex-col overflow-hidden rounded-2xl border border-border bg-popover text-popover-foreground shadow-[0_24px_80px_-20px_var(--player-shadow)] outline-none"
				>
					<DialogPrimitive.Title className="sr-only">
						Search your library
					</DialogPrimitive.Title>
					<div className="flex shrink-0 items-center gap-3 border-border border-b px-4">
						<Search className="size-5 shrink-0 text-caption" />
						<input
							ref={inputRef}
							type="search"
							role="combobox"
							aria-expanded={flatResults.length > 0}
							aria-controls={listboxId}
							aria-activedescendant={
								activeResult ? optionId(listboxId, activeResult.key) : undefined
							}
							aria-autocomplete="list"
							aria-label="Search your library"
							maxLength={200}
							placeholder="Search tracks, albums, artists, genres, playlists"
							value={query}
							onChange={(event) => {
								setQuery(event.target.value);
								setActiveKey(null);
							}}
							onKeyDown={onInputKeyDown}
							className="h-14 min-w-0 flex-1 bg-transparent text-base outline-none placeholder:text-muted-foreground"
						/>
						<kbd className="hidden rounded border border-border px-1.5 py-0.5 text-[10px] text-caption sm:block">
							Esc
						</kbd>
					</div>
					<div
						id={listboxId}
						aria-busy={isSearching}
						ref={listboxRef}
						role="listbox"
						aria-label="Search results"
						className="min-h-0 flex-1 overflow-y-auto"
					>
						<div ref={measureResults} className="flow-root p-2">
							{!hasQuery ? (
								<p className="px-3 py-6 text-center text-caption text-sm">
									Type to search your library.
								</p>
							) : null}
							{failed ? (
								<div role="alert" className="px-3 py-3 text-sm">
									<p>
										{flatResults.length > 0
											? "Search results could not be refreshed."
											: "Search results could not be loaded."}
									</p>
									<button
										type="button"
										className="mt-2 underline"
										disabled={isSearching}
										onClick={() => {
											void search.refetch();
										}}
									>
										Retry
									</button>
								</div>
							) : null}
							{showEmpty ? (
								<p className="px-3 py-6 text-center text-caption text-sm">
									Nothing in your library matches “{debouncedQuery}”.
								</p>
							) : null}
							{isSearching && flatResults.length === 0 ? (
								<p className="px-3 py-6 text-center text-caption text-sm">
									Searching…
								</p>
							) : null}
							{groups.length > 0 ? (
								<section aria-label="From Your Library">
									<p className="px-3 pt-2 pb-1 font-semibold text-[0.6875rem] text-caption uppercase tracking-wide">
										From Your Library
									</p>
									{groups.map((group) => (
										// biome-ignore lint/a11y/useSemanticElements: listbox option groups are not form fieldsets.
										<div
											key={group.label}
											role="group"
											aria-label={group.label}
											className="pb-1"
										>
											<p className="flex items-center gap-1.5 px-3 py-1 font-medium text-caption text-xs">
												<group.icon className="size-3.5" />
												{group.label}
											</p>
											<ul className="flex flex-col">
												{group.results.map((result) => {
													const isActive = activeResult?.key === result.key;
													return (
														<li key={result.key}>
															<button
																type="button"
																id={optionId(listboxId, result.key)}
																role="option"
																aria-selected={isActive}
																tabIndex={-1}
																className={cn(
																	"flex w-full cursor-pointer items-baseline gap-3 rounded-lg py-2 pr-3 pl-8 text-left text-sm",
																	isActive
																		? "bg-[var(--shell-active)] text-[var(--shell-active-foreground)]"
																		: "hover:bg-muted/50",
																)}
																onMouseEnter={() => setActiveKey(result.key)}
																onMouseDown={(event) => event.preventDefault()}
																onClick={result.action}
															>
																<span className="min-w-0 flex-1 truncate">
																	{result.title}
																</span>
																{result.subtitle ? (
																	<span className="max-w-[55%] truncate text-caption text-xs">
																		{result.subtitle}
																	</span>
																) : null}
															</button>
														</li>
													);
												})}
											</ul>
										</div>
									))}
								</section>
							) : null}
						</div>
					</div>
				</DialogPrimitive.Content>
			</DialogPrimitive.Portal>
		</DialogPrimitive.Root>
	);
}

function optionId(listboxId: string, key: string): string {
	return `${listboxId}-${encodeURIComponent(key)}`;
}

function isTypingTarget(target: EventTarget | null): boolean {
	if (!(target instanceof HTMLElement)) return false;
	return Boolean(target.closest("input, textarea, select, [contenteditable]"));
}

/** Ctrl/⌘+K toggles the dialog; "/" opens it unless a field is focused. */
function useLibrarySearchShortcut(
	open: boolean,
	onOpenChange: (open: boolean) => void,
) {
	useEffect(() => {
		const onKeyDown = (event: KeyboardEvent) => {
			if (event.defaultPrevented || event.repeat) return;
			const isCommandK =
				(event.ctrlKey || event.metaKey) &&
				!event.altKey &&
				!event.shiftKey &&
				event.key.toLowerCase() === "k";
			if (isCommandK) {
				event.preventDefault();
				onOpenChange(!open);
				return;
			}
			if (
				event.key === "/" &&
				!event.ctrlKey &&
				!event.metaKey &&
				!event.altKey &&
				!open &&
				!isTypingTarget(event.target)
			) {
				event.preventDefault();
				onOpenChange(true);
			}
		};
		window.addEventListener("keydown", onKeyDown);
		return () => window.removeEventListener("keydown", onKeyDown);
	}, [open, onOpenChange]);
}

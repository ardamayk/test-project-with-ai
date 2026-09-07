import type { Artist, Playlist, Track } from "@repo/api-client";
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
import {
	collectGenres,
	fetchGenreTracks,
	GENRE_SOURCE_QUERY_KEY,
} from "#/lib/collect-genres";
import { getAlbumArtistName, getTrackArtistName } from "#/lib/library-display";
import { playlistQueryKeys } from "#/lib/playlist-query-cache";
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

function includesQuery(haystack: string, query: string): boolean {
	return haystack.toLocaleLowerCase().includes(query.toLocaleLowerCase());
}

function pluralize(count: number, noun: string): string {
	return `${count} ${noun}${count === 1 ? "" : "s"}`;
}

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
	const [query, setQuery] = useState("");
	const [activeIndex, setActiveIndex] = useState(0);
	const debouncedQuery = useDebouncedValue(query.trim(), SEARCH_DEBOUNCE_MS);
	const hasQuery = query.trim().length > 0;
	const isDebouncing = query.trim() !== debouncedQuery;
	const canShowResults = open && hasQuery && !isDebouncing;
	const inputRef = useRef<HTMLInputElement>(null);
	const listboxRef = useRef<HTMLDivElement>(null);
	const returnFocus = useReturnFocus();
	const navigate = useNavigate();
	const { playTrack } = usePlayback();
	const listboxId = useId();

	useLibrarySearchShortcut(open, onOpenChange);

	// Reset for the next opening; keep the last query while closed only
	// long enough for the close animation.
	useEffect(() => {
		if (!open) {
			setQuery("");
			setActiveIndex(0);
		}
	}, [open]);

	const tracks = useQuery({
		queryKey: ["library", "search", "tracks", debouncedQuery],
		queryFn: () =>
			apiClient.listTracks({ limit: RESULTS_PER_GROUP, q: debouncedQuery }),
		enabled: canShowResults,
	});
	const albums = useQuery({
		queryKey: ["library", "search", "albums", debouncedQuery],
		queryFn: () =>
			apiClient.listAlbums({ limit: RESULTS_PER_GROUP, q: debouncedQuery }),
		enabled: canShowResults,
	});
	const artists = useQuery({
		queryKey: ["library", "search", "artists", debouncedQuery],
		queryFn: () =>
			apiClient.listArtists({ limit: RESULTS_PER_GROUP, q: debouncedQuery }),
		enabled: canShowResults,
	});
	// Genres and playlists have no server-side search: filter the cached
	// lists the Genres and Playlists pages already load.
	const genreSource = useQuery({
		queryKey: GENRE_SOURCE_QUERY_KEY,
		queryFn: () => fetchGenreTracks(apiClient.listTracks),
		staleTime: 60_000,
		enabled: canShowResults,
	});
	const playlists = useQuery({
		queryKey: playlistQueryKeys.list,
		queryFn: () => apiClient.listPlaylists(),
		enabled: canShowResults,
	});

	const close = useCallback(() => onOpenChange(false), [onOpenChange]);

	const groups = useMemo<SearchGroup[]>(() => {
		if (!canShowResults) return [];
		const trackResults = (tracks.data?.items ?? []).map(
			(track: Track): SearchResult => ({
				key: `track:${track.id}`,
				title: track.title,
				subtitle: getTrackArtistName(track),
				action: () => {
					void playTrack(track.id);
					close();
				},
			}),
		);
		const openAlbum = (albumId: string) => () => {
			void navigate({ to: "/library/$albumId", params: { albumId } });
			close();
		};
		// Albums match by title on the server; the albums that hold matching
		// tracks belong here too (searching "Nemo" should surface "Decades").
		const albumResults: SearchResult[] = [];
		const seenAlbums = new Set<string>();
		for (const album of albums.data?.items ?? []) {
			seenAlbums.add(album.id);
			albumResults.push({
				key: `album:${album.id}`,
				title: album.title,
				subtitle: getAlbumArtistName(album),
				action: openAlbum(album.id),
			});
		}
		for (const track of tracks.data?.items ?? []) {
			if (!track.albumId || seenAlbums.has(track.albumId)) continue;
			seenAlbums.add(track.albumId);
			albumResults.push({
				key: `album:${track.albumId}`,
				title: track.albumTitle ?? "Unknown album",
				subtitle: getTrackArtistName(track),
				action: openAlbum(track.albumId),
			});
		}
		albumResults.splice(RESULTS_PER_GROUP);
		const artistResults = (artists.data?.items ?? []).map(
			(artist: Artist): SearchResult => ({
				key: `artist:${artist.id}`,
				title: artist.name,
				subtitle:
					artist.albumCount != null
						? pluralize(artist.albumCount, "album")
						: undefined,
				action: () => {
					void navigate({
						to: "/library/artists",
						search: { q: artist.name },
					});
					close();
				},
			}),
		);
		const genreResults = collectGenres(genreSource.data?.items ?? [])
			.filter((genre) => includesQuery(genre.name, debouncedQuery))
			.slice(0, RESULTS_PER_GROUP)
			.map(
				(genre): SearchResult => ({
					key: `genre:${genre.name.toLowerCase()}`,
					title: genre.name,
					subtitle: pluralize(genre.trackCount, "track"),
					action: () => {
						void navigate({
							to: "/library/genres/$genre",
							params: { genre: genre.name },
						});
						close();
					},
				}),
			);
		const playlistResults = (playlists.data?.items ?? [])
			.filter((playlist: Playlist) =>
				includesQuery(playlist.name, debouncedQuery),
			)
			.slice(0, RESULTS_PER_GROUP)
			.map(
				(playlist: Playlist): SearchResult => ({
					key: `playlist:${playlist.id}`,
					title: playlist.name,
					subtitle: pluralize(playlist.trackCount, "track"),
					action: () => {
						void navigate({
							to: "/playlists/$playlistId",
							params: { playlistId: playlist.id },
						});
						close();
					},
				}),
			);
		return [
			{ label: "Track", icon: Music2, results: trackResults },
			{ label: "Album", icon: Disc3, results: albumResults },
			{ label: "Artist", icon: Users, results: artistResults },
			{ label: "Genre", icon: Tags, results: genreResults },
			{ label: "Playlist", icon: ListMusic, results: playlistResults },
		].filter((group) => group.results.length > 0);
	}, [
		canShowResults,
		debouncedQuery,
		tracks.data,
		albums.data,
		artists.data,
		genreSource.data,
		playlists.data,
		navigate,
		playTrack,
		close,
	]);

	const flatResults = useMemo(
		() => groups.flatMap((group) => group.results),
		[groups],
	);
	const activeResult = flatResults[activeIndex] ?? flatResults[0];
	const searchQueries = [tracks, albums, artists, genreSource, playlists];
	const failedQueries = canShowResults
		? searchQueries.filter((result) => result.isError)
		: [];
	const isSearching =
		hasQuery &&
		(isDebouncing || searchQueries.some((result) => result.isFetching));
	const showEmpty =
		canShowResults &&
		!isSearching &&
		failedQueries.length === 0 &&
		flatResults.length === 0;

	useEffect(() => {
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

	const onInputKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
		if (flatResults.length === 0) return;
		if (event.key === "ArrowDown") {
			event.preventDefault();
			setActiveIndex((index) => (index + 1) % flatResults.length);
		} else if (event.key === "ArrowUp") {
			event.preventDefault();
			setActiveIndex(
				(index) => (index - 1 + flatResults.length) % flatResults.length,
			);
		} else if (event.key === "Enter") {
			event.preventDefault();
			activeResult?.action();
		}
	};

	return (
		<DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
			<DialogPrimitive.Portal>
				{/* Transparent: the page stays as it is behind the search card. */}
				<DialogPrimitive.Overlay className="fixed inset-0 z-50" />
				<DialogPrimitive.Content
					data-testid="library-search-dialog"
					onOpenAutoFocus={(event) => {
						returnFocus.capture();
						event.preventDefault();
						inputRef.current?.focus();
					}}
					onCloseAutoFocus={returnFocus.restore}
					aria-describedby={undefined}
					className="fixed top-1/2 left-1/2 z-50 flex max-h-[70vh] w-[min(40rem,92vw)] -translate-x-1/2 -translate-y-1/2 flex-col overflow-hidden rounded-2xl border border-border bg-popover text-popover-foreground shadow-[0_24px_80px_-20px_var(--player-shadow)] outline-none"
				>
					<DialogPrimitive.Title className="sr-only">
						Search your library
					</DialogPrimitive.Title>
					<div className="flex items-center gap-3 border-border border-b px-4">
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
							placeholder="Search tracks, albums, artists, genres, playlists"
							value={query}
							onChange={(event) => {
								setQuery(event.target.value);
								setActiveIndex(0);
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
						ref={listboxRef}
						role="listbox"
						aria-label="Search results"
						className="min-h-0 flex-1 overflow-y-auto p-2"
					>
						{!hasQuery ? (
							<p className="px-3 py-6 text-center text-caption text-sm">
								Type to search your library.
							</p>
						) : null}
						{failedQueries.length > 0 ? (
							<div role="alert" className="px-3 py-3 text-sm">
								<p>
									{flatResults.length > 0
										? "Some search results could not be loaded."
										: "Search results could not be loaded."}
								</p>
								<button
									type="button"
									className="mt-2 underline"
									disabled={isSearching}
									onClick={() => {
										for (const result of failedQueries) void result.refetch();
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
									<div key={group.label} className="pb-1">
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
															onMouseEnter={() =>
																setActiveIndex(flatResults.indexOf(result))
															}
															onMouseDown={(event) => event.preventDefault()}
															onClick={result.action}
														>
															<span className="min-w-0 flex-1 truncate">
																{result.title}
															</span>
															{result.subtitle ? (
																<span className="shrink-0 truncate text-caption text-xs">
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
				</DialogPrimitive.Content>
			</DialogPrimitive.Portal>
		</DialogPrimitive.Root>
	);
}

function optionId(listboxId: string, key: string): string {
	return `${listboxId}-${key.replace(/[^a-z0-9_-]/gi, "_")}`;
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

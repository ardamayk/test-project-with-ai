import { useQuery } from "@tanstack/react-query";
import { Search, SlidersHorizontal, X } from "lucide-react";
import { Dialog as DialogPrimitive } from "radix-ui";
import { useDeferredValue, useMemo, useState } from "react";
import {
	type AlbumFilterState,
	AlbumFilters,
	collectAlbumGenres,
	filterAlbums,
} from "#/components/album-filters";
import { AlbumGrid } from "#/components/album-grid";
import {
	HEADER_SEARCH_CONTAINER_CLASS,
	HEADER_SEARCH_INPUT_CLASS,
	PageHeader,
	PageShell,
} from "#/components/page-layout";
import { Button } from "#/components/ui/button";
import { Input } from "#/components/ui/input";
import { apiClient } from "#/lib/api";

const defaultFilters: AlbumFilterState = {
	albumQuery: "",
	artistId: "all",
	genre: "all",
};

const ALBUMS_CENTERED_CONTAINER_CLASS = "mx-auto w-full max-w-6xl";

export function AlbumsPage() {
	const [filters, setFilters] = useState<AlbumFilterState>(defaultFilters);
	const [areFiltersOpen, setAreFiltersOpen] = useState(false);
	const deferredAlbumQuery = useDeferredValue(filters.albumQuery.trim());

	const artists = useQuery({
		queryKey: ["library", "artists", "all"],
		queryFn: () => apiClient.listArtists({ limit: 500 }),
		staleTime: 60_000,
	});

	const genreSource = useQuery({
		queryKey: ["library", "albums", "genre-source"],
		queryFn: () => apiClient.listAlbums({ limit: 500 }),
		staleTime: 60_000,
	});

	const albums = useQuery({
		queryKey: ["library", "albums", deferredAlbumQuery, filters.artistId],
		queryFn: () =>
			apiClient.listAlbums({
				limit: 500,
				q: deferredAlbumQuery || undefined,
				artistId:
					filters.artistId && filters.artistId !== "all"
						? filters.artistId
						: undefined,
			}),
	});

	const genreOptions = useMemo(
		() => collectAlbumGenres(genreSource.data?.items ?? []),
		[genreSource.data?.items],
	);

	const visibleAlbums = useMemo(
		() => filterAlbums(albums.data?.items ?? [], filters),
		[albums.data?.items, filters],
	);

	return (
		<PageShell
			testId="albums-page-shell"
			contentTestId="albums-page-content"
			contentClassName="pt-8"
			header={
				<PageHeader
					title="Albums"
					description="Browse albums in your library"
					className="pt-7 pb-4"
					innerClassName={ALBUMS_CENTERED_CONTAINER_CLASS}
					actions={
						<>
							<div className={HEADER_SEARCH_CONTAINER_CLASS}>
								<Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-caption" />
								<Input
									className={HEADER_SEARCH_INPUT_CLASS}
									placeholder="Search albums..."
									value={filters.albumQuery}
									onChange={(e) =>
										setFilters({ ...filters, albumQuery: e.target.value })
									}
								/>
							</div>
							<Button
								type="button"
								variant="ghost"
								size="icon"
								aria-label="Filters"
								className="size-10 text-caption hover:text-heading"
								onClick={() => setAreFiltersOpen(true)}
							>
								<SlidersHorizontal className="size-5" />
							</Button>
						</>
					}
				/>
			}
		>
			<div className={ALBUMS_CENTERED_CONTAINER_CLASS}>
				<AlbumFilterDrawer
					isOpen={areFiltersOpen}
					artists={artists.data?.items ?? []}
					genreOptions={genreOptions}
					filters={filters}
					resultCount={visibleAlbums.length}
					onOpenChange={setAreFiltersOpen}
					onFiltersChange={setFilters}
				/>

				{albums.isLoading ? (
					<p className="text-foreground text-sm">Loading albums…</p>
				) : albums.isError ? (
					<p className="text-destructive text-sm">Failed to load albums</p>
				) : visibleAlbums.length === 0 ? (
					<p className="text-foreground text-sm">
						No albums match your filters.
					</p>
				) : (
					<AlbumGrid albums={visibleAlbums} />
				)}
			</div>
		</PageShell>
	);
}

function AlbumFilterDrawer({
	isOpen,
	artists,
	genreOptions,
	filters,
	resultCount,
	onOpenChange,
	onFiltersChange,
}: {
	isOpen: boolean;
	artists: Parameters<typeof AlbumFilters>[0]["artists"];
	genreOptions: string[];
	filters: AlbumFilterState;
	resultCount: number;
	onOpenChange: (isOpen: boolean) => void;
	onFiltersChange: (next: AlbumFilterState) => void;
}) {
	return (
		<DialogPrimitive.Root open={isOpen} onOpenChange={onOpenChange}>
			<DialogPrimitive.Portal>
				<DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-background/50 backdrop-blur-sm" />
				<DialogPrimitive.Content
					aria-describedby="album-filter-description"
					className="fixed top-0 right-0 z-50 flex h-dvh w-[min(390px,100vw)] flex-col gap-5 border-border border-l bg-background p-6 shadow-xl outline-none"
				>
					<div className="flex items-start justify-between gap-4">
						<div className="min-w-0">
							<DialogPrimitive.Title className="font-semibold text-heading text-xl">
								Album filters
							</DialogPrimitive.Title>
							<DialogPrimitive.Description
								id="album-filter-description"
								className="mt-1 text-caption text-sm"
							>
								Selections apply immediately.
							</DialogPrimitive.Description>
						</div>
						<DialogPrimitive.Close asChild>
							<Button type="button" variant="ghost" size="icon">
								<X className="size-4" />
								<span className="sr-only">Close</span>
							</Button>
						</DialogPrimitive.Close>
					</div>

					<div className="min-h-0 flex-1 overflow-y-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
						<AlbumFilters
							artists={artists}
							genreOptions={genreOptions}
							filters={filters}
							onFiltersChange={onFiltersChange}
							resultCount={resultCount}
							showSearch={false}
						/>
					</div>
				</DialogPrimitive.Content>
			</DialogPrimitive.Portal>
		</DialogPrimitive.Root>
	);
}

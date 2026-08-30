import { useQuery } from "@tanstack/react-query";
import { Search, TriangleAlert, Users } from "lucide-react";
import { useState } from "react";
import { ArtistGrid } from "#/components/artist-grid";
import {
	COLLECTION_PAGE_CONTAINER_CLASS,
	CollectionGridSkeleton,
	CollectionGridState,
	CollectionPageContainer,
} from "#/components/collection-grid-layout";
import {
	HEADER_SEARCH_CONTAINER_CLASS,
	HEADER_SEARCH_INPUT_CLASS,
	PageHeader,
	PageShell,
} from "#/components/page-layout";
import { Input } from "#/components/ui/input";
import { apiClient } from "#/lib/api";

export function ArtistsPage({
	initialSearch = "",
}: {
	initialSearch?: string;
}) {
	const [search, setSearch] = useState(initialSearch);
	const artists = useQuery({
		queryKey: ["library", "artists", search],
		queryFn: () =>
			apiClient.listArtists({ limit: 100, q: search || undefined }),
	});
	const artistItems = artists.data?.items ?? [];
	const isInitialLoading = artists.isLoading && !artists.data;
	const hasError = artists.isError && !artists.data;

	return (
		<PageShell
			testId="artists-page-shell"
			contentTestId="artists-page-content"
			header={
				<PageHeader
					title="Artists"
					description="Browse by artist"
					innerClassName={COLLECTION_PAGE_CONTAINER_CLASS}
					actions={
						<div className={HEADER_SEARCH_CONTAINER_CLASS}>
							<Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-caption" />
							<Input
								className={HEADER_SEARCH_INPUT_CLASS}
								placeholder="Search artists..."
								value={search}
								onChange={(event) => setSearch(event.target.value)}
							/>
						</div>
					}
				/>
			}
		>
			<CollectionPageContainer aria-busy={artists.isFetching || undefined}>
				{isInitialLoading ? (
					<CollectionGridSkeleton label="Loading artists" />
				) : hasError ? (
					<CollectionGridState
						kind="error"
						icon={<TriangleAlert aria-hidden />}
						title="Unable to load artists"
						description="Check your connection and try again."
						onRetry={() => void artists.refetch()}
						isRetrying={artists.isFetching}
					/>
				) : artistItems.length === 0 ? (
					<CollectionGridState
						kind="empty"
						icon={<Users aria-hidden />}
						title={
							search.trim() ? "No artists match your search" : "No artists yet"
						}
						description={
							search.trim()
								? "Try adjusting your search."
								: "Scan your library to get started."
						}
					/>
				) : (
					<ArtistGrid artists={artistItems} />
				)}
			</CollectionPageContainer>
		</PageShell>
	);
}

import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Search } from "lucide-react";
import { useState } from "react";
import { z } from "zod";
import { ArtistGrid } from "#/components/artist-grid";
import {
	HEADER_SEARCH_CONTAINER_CLASS,
	HEADER_SEARCH_INPUT_CLASS,
	PageHeader,
	PageShell,
} from "#/components/page-layout";
import { Input } from "#/components/ui/input";
import { apiClient } from "#/lib/api";

const artistsSearchSchema = z.object({
	q: z.string().optional(),
});

export const Route = createFileRoute("/library/artists/")({
	validateSearch: artistsSearchSchema,
	component: ArtistsPage,
});

function ArtistsPage() {
	const { q } = Route.useSearch();
	const [search, setSearch] = useState(q ?? "");
	const artists = useQuery({
		queryKey: ["library", "artists", search],
		queryFn: () =>
			apiClient.listArtists({ limit: 100, q: search || undefined }),
	});

	return (
		<PageShell
			testId="artists-page-shell"
			contentTestId="artists-page-content"
			header={
				<PageHeader
					title="Artists"
					description="Browse by artist"
					actions={
						<div className={HEADER_SEARCH_CONTAINER_CLASS}>
							<Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-caption" />
							<Input
								className={HEADER_SEARCH_INPUT_CLASS}
								placeholder="Search artists..."
								value={search}
								onChange={(e) => setSearch(e.target.value)}
							/>
						</div>
					}
				/>
			}
		>
			{artists.isLoading ? (
				<p className="text-foreground text-sm">Loading artists…</p>
			) : artists.isError ? (
				<p className="text-destructive text-sm">Failed to load artists</p>
			) : (
				<ArtistGrid artists={artists.data?.items ?? []} />
			)}
		</PageShell>
	);
}

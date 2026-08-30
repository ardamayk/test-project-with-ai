import type { Artist } from "@repo/api-client";
import { CollectionGrid } from "#/components/collection-grid-layout";

export function ArtistGrid({ artists }: { artists: Artist[] }) {
	return (
		<CollectionGrid>
			{artists.map((artist) => (
				<div
					key={artist.id}
					className="group flex aspect-square min-w-0 flex-col overflow-hidden rounded-lg border border-border p-3 transition hover:bg-muted/50"
				>
					<div className="mb-3 flex min-h-0 flex-1 items-center justify-center rounded-md bg-muted font-semibold text-2xl uppercase">
						{artist.name.slice(0, 1)}
					</div>
					<p className="truncate font-medium text-heading text-sm group-hover:underline">
						{artist.name}
					</p>
					{artist.albumCount != null ? (
						<p className="text-foreground text-xs">
							{artist.albumCount} albums
						</p>
					) : null}
				</div>
			))}
		</CollectionGrid>
	);
}

import type { Album } from "@repo/api-client";
import { AlbumArt } from "@repo/ui";
import { Link } from "@tanstack/react-router";
import { CollectionGrid } from "#/components/collection-grid-layout";
import {
	ContextMenu,
	ContextMenuContent,
	ContextMenuItem,
	ContextMenuTrigger,
} from "#/components/ui/context-menu";
import { apiClient } from "#/lib/api";
import { getAlbumArtistName } from "#/lib/library-display";

export function AlbumGrid({ albums }: { albums: Album[] }) {
	return (
		<CollectionGrid>
			{albums.map((album) => (
				<ContextMenu key={album.id}>
					<ContextMenuTrigger asChild>
						<Link
							to="/library/$albumId"
							params={{ albumId: album.id }}
							className="group relative aspect-square overflow-hidden rounded-md border border-border bg-card transition duration-300 ease-out hover:-translate-y-1 hover:bg-muted/50 hover:shadow-lg"
						>
							<AlbumArt
								coverUrl={apiClient.getAlbumCoverUrl(album.id)}
								title={album.title}
								className="absolute inset-0 size-full text-2xl transition duration-300 group-hover:scale-105"
							/>
							<div
								data-album-card-overlay
								className="absolute inset-x-0 bottom-0 z-10 bg-gradient-to-t from-background/95 via-background/75 to-transparent p-2 text-foreground"
							>
								<p className="truncate font-medium text-heading text-xs leading-tight group-hover:underline">
									{album.title}
								</p>
								<p className="truncate text-[11px] text-foreground leading-tight">
									{getAlbumArtistName(album)}
								</p>
								{album.releaseDate != null || album.year != null ? (
									<p className="text-[10px] text-caption leading-tight">
										{album.releaseDate ?? album.year}
									</p>
								) : null}
							</div>
						</Link>
					</ContextMenuTrigger>
					<ContextMenuContent>
						<ContextMenuItem asChild>
							<Link to="/library/$albumId" params={{ albumId: album.id }}>
								Open album
							</Link>
						</ContextMenuItem>
					</ContextMenuContent>
				</ContextMenu>
			))}
		</CollectionGrid>
	);
}

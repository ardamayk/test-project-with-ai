import type { Album } from "@repo/api-client";
import { AlbumArt } from "@repo/ui";
import { Link } from "@tanstack/react-router";
import { Trash2 } from "lucide-react";
import { CollectionGrid } from "#/components/collection-grid-layout";
import {
	ContextMenu,
	ContextMenuContent,
	ContextMenuItem,
	ContextMenuSeparator,
	ContextMenuTrigger,
} from "#/components/ui/context-menu";
import { confirmDelete, useDeleteAlbum } from "#/hooks/use-delete-library";
import { apiClient } from "#/lib/api";

export function AlbumGrid({ albums }: { albums: Album[] }) {
	const deleteAlbum = useDeleteAlbum();

	const handleDelete = (album: Album) => {
		const confirmed = confirmDelete(
			`Delete "${album.title}" by ${album.artistName}?\n\nThis removes the album, all of its tracks, and their files from disk.`,
		);
		if (!confirmed) return;
		deleteAlbum.mutate(album.id);
	};

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
									{album.artistName}
								</p>
								{album.year != null ? (
									<p className="text-[10px] text-caption leading-tight">
										{album.year}
									</p>
								) : null}
							</div>
						</Link>
					</ContextMenuTrigger>
					<ContextMenuContent>
						<ContextMenuItem
							variant="destructive"
							disabled={deleteAlbum.isPending}
							onSelect={() => handleDelete(album)}
						>
							<Trash2 className="size-4" />
							Delete album
						</ContextMenuItem>
						<ContextMenuSeparator />
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

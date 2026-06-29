import type { Album } from '@repo/api-client'
import { AlbumArt } from '@repo/ui'
import { Link } from '@tanstack/react-router'
import { Trash2 } from 'lucide-react'
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from '#/components/ui/context-menu'
import {
  confirmDelete,
  useDeleteAlbum,
} from '#/hooks/use-delete-library'
import { apiClient } from '#/lib/api'

export function AlbumGrid({ albums }: { albums: Album[] }) {
  const deleteAlbum = useDeleteAlbum()

  if (albums.length === 0) {
    return (
      <p className="text-foreground text-sm">
        No albums yet. Scan your library to get started.
      </p>
    )
  }

  const handleDelete = (album: Album) => {
    const confirmed = confirmDelete(
      `Delete "${album.title}" by ${album.artistName}?\n\nThis removes the album, all of its tracks, and their files from disk.`,
    )
    if (!confirmed) return
    deleteAlbum.mutate(album.id)
  }

  return (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
      {albums.map((album) => (
        <ContextMenu key={album.id}>
          <ContextMenuTrigger asChild>
            <Link
              to="/library/$albumId"
              params={{ albumId: album.id }}
              className="group rounded-lg border border-border p-3 transition hover:bg-muted/50"
            >
              <AlbumArt
                coverUrl={apiClient.getAlbumCoverUrl(album.id)}
                title={album.title}
                className="mb-3 aspect-square w-full rounded-md text-2xl"
              />
              <p className="truncate font-medium text-heading text-sm group-hover:underline">
                {album.title}
              </p>
              <p className="truncate text-foreground text-xs">
                {album.artistName}
                {album.trackCount != null ? ` · ${album.trackCount} tracks` : ''}
              </p>
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
    </div>
  )
}

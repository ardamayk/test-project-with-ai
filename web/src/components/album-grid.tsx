import type { Album } from '@repo/api-client'
import { AlbumArt } from '@repo/ui'
import { Link } from '@tanstack/react-router'
import { apiClient } from '#/lib/api'

export function AlbumGrid({ albums }: { albums: Album[] }) {
  if (albums.length === 0) {
    return (
      <p className="text-foreground text-sm">
        No albums yet. Scan your library to get started.
      </p>
    )
  }

  return (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
      {albums.map((album) => (
        <Link
          key={album.id}
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
      ))}
    </div>
  )
}

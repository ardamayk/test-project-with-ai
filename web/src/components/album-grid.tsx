import type { Album } from '@repo/api-client'
import { Link } from '@tanstack/react-router'

export function AlbumGrid({ albums }: { albums: Album[] }) {
  if (albums.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
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
          <div className="mb-3 flex aspect-square items-center justify-center rounded-md bg-muted font-semibold text-2xl uppercase">
            {album.title.slice(0, 1)}
          </div>
          <p className="truncate font-medium text-sm group-hover:underline">
            {album.title}
          </p>
          <p className="truncate text-muted-foreground text-xs">
            {album.artistName}
            {album.trackCount != null ? ` · ${album.trackCount} tracks` : ''}
          </p>
        </Link>
      ))}
    </div>
  )
}

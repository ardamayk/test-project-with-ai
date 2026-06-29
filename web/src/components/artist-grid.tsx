import type { Artist } from '@repo/api-client'

export function ArtistGrid({ artists }: { artists: Artist[] }) {
  if (artists.length === 0) {
    return (
      <p className="text-foreground text-sm">
        No artists yet. Scan your library to get started.
      </p>
    )
  }

  return (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
      {artists.map((artist) => (
        <div
          key={artist.id}
          className="group rounded-lg border border-border p-3 transition hover:bg-muted/50"
        >
          <div className="mb-3 flex aspect-square items-center justify-center rounded-md bg-muted font-semibold text-2xl uppercase">
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
    </div>
  )
}

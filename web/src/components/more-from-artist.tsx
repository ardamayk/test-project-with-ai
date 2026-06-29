import type { Album } from '@repo/api-client'
import { AlbumArt } from '@repo/ui'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useRef } from 'react'
import { Button } from '#/components/ui/button'
import { apiClient } from '#/lib/api'

function sortAlbumsByYear(albums: Album[]): Album[] {
  return [...albums].sort((a, b) => {
    const yearA = a.year ?? -1
    const yearB = b.year ?? -1
    if (yearA !== yearB) return yearB - yearA
    return a.title.localeCompare(b.title, undefined, { sensitivity: 'base' })
  })
}

export function MoreFromArtist({
  artistId,
  artistName,
  excludeAlbumId,
}: {
  artistId: string
  artistName: string
  excludeAlbumId: string
}) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const albums = useQuery({
    queryKey: ['library', 'albums', 'artist', artistId],
    queryFn: () => apiClient.listAlbums({ artistId, limit: 100 }),
    staleTime: 60_000,
  })

  const items = sortAlbumsByYear(
    (albums.data?.items ?? []).filter((album) => album.id !== excludeAlbumId),
  )

  if (albums.isLoading) {
    return (
      <section className="mt-10">
        <h2 className="mb-4 font-semibold text-heading text-xl">
          More from {artistName}
        </h2>
        <p className="text-foreground text-sm">Loading albums…</p>
      </section>
    )
  }

  if (items.length === 0) {
    return null
  }

  const scrollBy = (direction: -1 | 1) => {
    const node = scrollRef.current
    if (!node) return
    node.scrollBy({ left: direction * 320, behavior: 'smooth' })
  }

  return (
    <section className="mt-10">
      <div className="mb-4 flex items-center justify-between gap-3">
        <h2 className="font-semibold text-heading text-xl">
          More from {artistName}
        </h2>
        <div className="flex items-center gap-1">
          <Button
            type="button"
            variant="outline"
            size="icon"
            className="size-8"
            aria-label="Scroll left"
            onClick={() => scrollBy(-1)}
          >
            <ChevronLeft className="size-4" />
          </Button>
          <Button
            type="button"
            variant="outline"
            size="icon"
            className="size-8"
            aria-label="Scroll right"
            onClick={() => scrollBy(1)}
          >
            <ChevronRight className="size-4" />
          </Button>
        </div>
      </div>

      <div
        ref={scrollRef}
        className="flex gap-4 overflow-x-auto pb-2 [-ms-overflow-style:none] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
      >
        {items.map((album) => (
          <Link
            key={album.id}
            to="/library/$albumId"
            params={{ albumId: album.id }}
            className="group w-40 shrink-0 sm:w-44"
          >
            <AlbumArt
              coverUrl={apiClient.getAlbumCoverUrl(album.id)}
              title={album.title}
              className="mb-3 aspect-square w-full rounded-md text-2xl shadow-sm transition group-hover:ring-2 group-hover:ring-primary/40"
            />
            <p className="truncate font-medium text-heading text-sm group-hover:underline">
              {album.title}
            </p>
            <p className="truncate text-caption text-xs">
              {album.artistName}
              {album.year != null ? ` · ${album.year}` : ''}
            </p>
          </Link>
        ))}
      </div>
    </section>
  )
}

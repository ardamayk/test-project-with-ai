import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'
import { Search } from 'lucide-react'
import { ArtistGrid } from '#/components/artist-grid'
import { Input } from '#/components/ui/input'
import { apiClient } from '#/lib/api'

const artistsSearchSchema = z.object({
  q: z.string().optional(),
})

export const Route = createFileRoute('/library/artists/')({
  validateSearch: artistsSearchSchema,
  component: ArtistsPage,
})

function ArtistsPage() {
  const { q } = Route.useSearch()
  const [search, setSearch] = useState(q ?? '')
  const artists = useQuery({
    queryKey: ['library', 'artists', search],
    queryFn: () => apiClient.listArtists({ limit: 100, q: search || undefined }),
  })

  return (
    <div className="p-6">
      <header className="mb-6">
        <h1 className="font-semibold text-2xl">Artists</h1>
        <p className="text-foreground text-sm">Browse by artist</p>
      </header>

      <div className="relative mb-6 max-w-md">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-caption" />
        <Input
          className="pl-9"
          placeholder="Search artists…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </div>

      {artists.isLoading ? (
        <p className="text-foreground text-sm">Loading artists…</p>
      ) : artists.isError ? (
        <p className="text-destructive text-sm">Failed to load artists</p>
      ) : (
        <ArtistGrid artists={artists.data?.items ?? []} />
      )}
    </div>
  )
}

import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { ArtistGrid } from '#/components/artist-grid'
import { MainHeader } from '#/components/main-header'
import { apiClient } from '#/lib/api'

export const Route = createFileRoute('/library/artists/')({
  component: ArtistsPage,
})

function ArtistsPage() {
  const [search, setSearch] = useState('')
  const artists = useQuery({
    queryKey: ['library', 'artists', search],
    queryFn: () => apiClient.listArtists({ limit: 100, q: search || undefined }),
  })

  return (
    <>
      <MainHeader search={search} onSearchChange={setSearch} />
      <div className="p-6">
        <header className="mb-6">
          <h1 className="font-semibold text-2xl">Artists</h1>
          <p className="text-muted-foreground text-sm">Browse by artist</p>
        </header>
        {artists.isLoading ? (
          <p className="text-muted-foreground text-sm">Loading artists…</p>
        ) : artists.isError ? (
          <p className="text-destructive text-sm">Failed to load artists</p>
        ) : (
          <ArtistGrid artists={artists.data?.items ?? []} />
        )}
      </div>
    </>
  )
}

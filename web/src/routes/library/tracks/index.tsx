import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { MainHeader } from '#/components/main-header'
import { TrackList } from '#/components/track-list'
import { apiClient } from '#/lib/api'

export const Route = createFileRoute('/library/tracks/')({
  component: TracksPage,
})

function TracksPage() {
  const [search, setSearch] = useState('')
  const tracks = useQuery({
    queryKey: ['library', 'tracks', search],
    queryFn: () => apiClient.listTracks({ limit: 200, q: search || undefined }),
  })

  return (
    <>
      <MainHeader search={search} onSearchChange={setSearch} />
      <div className="p-6">
        <header className="mb-6">
          <h1 className="font-semibold text-2xl">Tracks</h1>
          <p className="text-muted-foreground text-sm">All tracks in your library</p>
        </header>
        {tracks.isLoading ? (
          <p className="text-muted-foreground text-sm">Loading tracks…</p>
        ) : tracks.isError ? (
          <p className="text-destructive text-sm">Failed to load tracks</p>
        ) : (
          <TrackList tracks={tracks.data?.items ?? []} />
        )}
      </div>
    </>
  )
}

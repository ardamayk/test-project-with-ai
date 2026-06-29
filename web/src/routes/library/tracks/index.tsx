import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { Search } from 'lucide-react'
import { TrackList } from '#/components/track-list'
import { Input } from '#/components/ui/input'
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
    <div className="p-6">
      <header className="mb-6">
        <h1 className="font-semibold text-2xl">Tracks</h1>
        <p className="text-foreground text-sm">All tracks in your library</p>
      </header>

      <div className="relative mb-6 max-w-md">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-caption" />
        <Input
          className="pl-9"
          placeholder="Search tracks…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </div>

      {tracks.isLoading ? (
        <p className="text-foreground text-sm">Loading tracks…</p>
      ) : tracks.isError ? (
        <p className="text-destructive text-sm">Failed to load tracks</p>
      ) : (
        <TrackList tracks={tracks.data?.items ?? []} showMeta />
      )}
    </div>
  )
}

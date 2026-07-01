import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { ListMusic, Star } from 'lucide-react'
import { apiClient } from '#/lib/api'

export const Route = createFileRoute('/playlists/')({
  component: PlaylistsPage,
})

function PlaylistsPage() {
  const playlists = useQuery({
    queryKey: ['playlists'],
    queryFn: () => apiClient.listPlaylists(),
  })

  if (playlists.isLoading) {
    return <div className="p-6 text-foreground text-sm">Loading playlists…</div>
  }

  if (playlists.isError) {
    return <div className="p-6 text-destructive text-sm">Failed to load playlists</div>
  }

  return (
    <div className="p-6">
      <div className="mb-6">
        <h1 className="font-semibold text-2xl text-heading">Playlists</h1>
        <p className="text-foreground text-sm">
          Favorites is your default playlist.
        </p>
      </div>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {(playlists.data?.items ?? []).map((playlist) => {
          const Icon = playlist.isDefault ? Star : ListMusic
          return (
            <div
              key={playlist.id}
              className="flex items-center gap-3 rounded-lg border border-border bg-card/40 p-4"
            >
              <div className="flex size-10 items-center justify-center rounded-md bg-muted text-caption">
                <Icon className="size-5" />
              </div>
              <div className="min-w-0">
                <p className="truncate font-medium text-heading">
                  {playlist.name}
                </p>
                <p className="text-caption text-xs">
                  {playlist.trackCount} track{playlist.trackCount === 1 ? '' : 's'}
                </p>
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

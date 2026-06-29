import { useQuery } from '@tanstack/react-query'
import { Link, createFileRoute } from '@tanstack/react-router'
import { TrackList } from '#/components/track-list'
import { apiClient } from '#/lib/api'

export const Route = createFileRoute('/library/$albumId')({
  component: AlbumDetailPage,
})

function AlbumDetailPage() {
  const { albumId } = Route.useParams()
  const album = useQuery({
    queryKey: ['library', 'album', albumId],
    queryFn: () => apiClient.getAlbum(albumId),
  })

  if (album.isLoading) {
    return <div className="p-6 text-muted-foreground text-sm">Loading album…</div>
  }

  if (album.isError || !album.data) {
    return <div className="p-6 text-destructive text-sm">Album not found</div>
  }

  const data = album.data

  return (
    <div className="p-6">
      <Link
        to="/library"
        className="mb-4 inline-block text-muted-foreground text-sm hover:text-foreground"
      >
        ← Back to library
      </Link>
      <header className="mb-6 flex items-end gap-4">
        <div className="flex size-32 shrink-0 items-center justify-center rounded-lg bg-muted font-semibold text-4xl uppercase">
          {data.title.slice(0, 1)}
        </div>
        <div>
          <p className="text-muted-foreground text-sm">{data.artistName}</p>
          <h1 className="font-semibold text-2xl">{data.title}</h1>
          {data.year != null && (
            <p className="text-muted-foreground text-sm">{data.year}</p>
          )}
        </div>
      </header>
      <TrackList tracks={data.tracks} albumId={data.id} />
    </div>
  )
}

import { useQuery } from '@tanstack/react-query'
import { Link, createFileRoute } from '@tanstack/react-router'
import { AlbumDetailHeader } from '#/components/album-detail-header'
import { MoreFromArtist } from '#/components/more-from-artist'
import { TrackList } from '#/components/track-list'
import { apiClient } from '#/lib/api'
import { usePlayback } from '@repo/ui'

export const Route = createFileRoute('/library/$albumId')({
  component: AlbumDetailPage,
})

function AlbumDetailPage() {
  const { albumId } = Route.useParams()
  const { playTrack } = usePlayback()
  const album = useQuery({
    queryKey: ['library', 'album', albumId],
    queryFn: () => apiClient.getAlbum(albumId),
    staleTime: 0,
  })

  if (album.isLoading) {
    return <div className="p-6 text-foreground text-sm">Loading album…</div>
  }

  if (album.isError || !album.data) {
    return <div className="p-6 text-destructive text-sm">Album not found</div>
  }

  const data = album.data

  const handlePlayAlbum = () => {
    const first = data.tracks[0]
    if (!first) return
    void playTrack(
      first.id,
      data.tracks.map((track) => track.id),
    )
  }

  return (
    <div className="p-6">
      <Link
        to="/library/albums"
        className="mb-5 inline-block text-foreground text-sm hover:text-heading"
      >
        ← Back to library
      </Link>

      <AlbumDetailHeader album={data} onPlayAlbum={handlePlayAlbum} />

      <section className="mt-6">
        <TrackList
          tracks={data.tracks}
          albumId={data.id}
          showFavorite
          showMeta
          compact
        />
      </section>

      <MoreFromArtist
        artistId={data.artistId}
        artistName={data.artistName}
        excludeAlbumId={data.id}
      />
    </div>
  )
}

import type { Track } from '@repo/api-client'
import { Clock, Heart, Trash2 } from 'lucide-react'
import { usePlayback } from '@repo/ui'
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from '#/components/ui/context-menu'
import { useFavoriteTracks } from '#/hooks/use-favorite-tracks'
import {
  confirmDelete,
  useDeleteTrack,
} from '#/hooks/use-delete-library'
import { cn } from '#/lib/utils'

function formatDuration(ms: number): string {
  if (!ms || ms < 0) return '0:00'
  const total = Math.floor(ms / 1000)
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${s.toString().padStart(2, '0')}`
}

export function TrackList({
  tracks,
  albumId,
  showFavorite = false,
  showGenre = false,
}: {
  tracks: Track[]
  albumId?: string
  showFavorite?: boolean
  showGenre?: boolean
}) {
  const { playTrack, currentTrack } = usePlayback()
  const { isFavorite, toggleFavorite } = useFavoriteTracks()
  const deleteTrack = useDeleteTrack()

  const handlePlay = (track: Track) => {
    const queueTrackIds = tracks.map((t) => t.id)
    void playTrack(track.id, queueTrackIds)
  }

  const handleDelete = (track: Track) => {
    const confirmed = confirmDelete(
      `Delete "${track.title}"?\n\nThis removes the track from your library and deletes its file from disk.`,
    )
    if (!confirmed) return
    deleteTrack.mutate(track.id)
  }

  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="border-border border-b text-left text-caption text-xs">
          <th className="w-12 px-3 py-2 font-medium">#</th>
          <th className="px-3 py-2 font-medium">Title</th>
          <th className="w-20 px-3 py-2 text-right font-medium">
            <span className="sr-only">Duration</span>
            <Clock className="ml-auto size-3.5" />
          </th>
          {showFavorite ? (
            <th className="w-12 px-2 py-2 text-center font-medium">
              <span className="sr-only">Favorite</span>
              <Heart className="mx-auto size-3.5" />
            </th>
          ) : null}
        </tr>
      </thead>
      <tbody>
        {tracks.map((track, index) => {
          const isPlaying = currentTrack?.id === track.id
          const favorited = isFavorite(track.id)

          return (
            <ContextMenu key={track.id}>
              <ContextMenuTrigger asChild>
                <tr
                  className={cn(
                    'group cursor-pointer border-border/50 border-b transition hover:bg-muted/50',
                    isPlaying && 'bg-primary/5',
                  )}
                  onClick={() => handlePlay(track)}
                >
                  <td className="px-3 py-2.5 text-caption tabular-nums">
                    {track.trackNo ?? index + 1}
                  </td>
                  <td className="px-3 py-2.5">
                    <span
                      className={cn(
                        'font-medium',
                        isPlaying ? 'text-heading' : 'text-foreground',
                      )}
                    >
                      {track.title}
                    </span>
                    {showGenre && track.genre ? (
                      <span className="block text-foreground text-xs">
                        {track.genre}
                      </span>
                    ) : null}
                    {!albumId ? (
                      <span className="block text-foreground text-xs">
                        {track.artistName}
                      </span>
                    ) : null}
                  </td>
                  <td className="px-3 py-2.5 text-right text-caption tabular-nums">
                    {formatDuration(track.durationMs)}
                  </td>
                  {showFavorite ? (
                    <td className="px-2 py-2.5 text-center">
                      <button
                        type="button"
                        aria-label={
                          favorited ? 'Remove from favorites' : 'Add to favorites'
                        }
                        className={cn(
                          'inline-flex size-8 items-center justify-center rounded-full text-caption transition hover:bg-muted hover:text-heading',
                          favorited && 'text-heading',
                        )}
                        onClick={(event) => {
                          event.stopPropagation()
                          toggleFavorite(track.id)
                        }}
                      >
                        <Heart
                          className="size-4"
                          fill={favorited ? 'currentColor' : 'none'}
                        />
                      </button>
                    </td>
                  ) : null}
                </tr>
              </ContextMenuTrigger>
              <ContextMenuContent>
                <ContextMenuItem
                  variant="destructive"
                  disabled={deleteTrack.isPending}
                  onSelect={() => handleDelete(track)}
                >
                  <Trash2 className="size-4" />
                  Delete track
                </ContextMenuItem>
              </ContextMenuContent>
            </ContextMenu>
          )
        })}
      </tbody>
    </table>
  )
}

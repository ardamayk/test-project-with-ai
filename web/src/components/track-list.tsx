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
import { formatTrackMeta } from '#/lib/format-track-meta'
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
  showMeta = false,
  compact = false,
}: {
  tracks: Track[]
  albumId?: string
  showFavorite?: boolean
  showMeta?: boolean
  compact?: boolean
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

  const rowPadding = compact ? 'px-3 py-1.5' : 'px-3 py-2.5'
  const favoritePadding = compact ? 'px-2 py-1.5' : 'px-2 py-2.5'

  return (
    <table className={cn('w-full', compact ? 'text-xs' : 'text-sm')}>
      <thead>
        <tr className="border-border border-b text-left text-caption text-[11px]">
          <th className={cn('w-10 font-medium', rowPadding)}>#</th>
          <th className={cn('font-medium', rowPadding)}>Title</th>
          <th className={cn('w-16 text-right font-medium', rowPadding)}>
            <span className="sr-only">Duration</span>
            <Clock className="ml-auto size-3.5" />
          </th>
          {showFavorite ? (
            <th className={cn('w-10 text-center font-medium', favoritePadding)}>
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
          const meta = showMeta ? formatTrackMeta(track) : null

          return (
            <ContextMenu key={track.id}>
              <ContextMenuTrigger asChild>
                <tr
                  className={cn(
                    'group cursor-pointer border-border/40 border-b transition hover:bg-muted/50',
                    isPlaying && 'bg-primary/5',
                  )}
                  onClick={() => handlePlay(track)}
                >
                  <td className={cn('text-caption tabular-nums', rowPadding)}>
                    {track.trackNo ?? index + 1}
                  </td>
                  <td className={rowPadding}>
                    <span
                      className={cn(
                        'font-medium leading-snug',
                        compact ? 'text-sm' : 'text-base',
                        isPlaying ? 'text-heading' : 'text-foreground',
                      )}
                    >
                      {track.title}
                    </span>
                    {meta ? (
                      <span className="mt-0.5 block text-[11px] text-caption leading-tight">
                        {meta}
                      </span>
                    ) : null}
                    {!albumId ? (
                      <span className="mt-0.5 block text-caption text-[11px] leading-tight">
                        {track.artistName}
                      </span>
                    ) : null}
                  </td>
                  <td
                    className={cn(
                      'text-right text-caption tabular-nums',
                      rowPadding,
                    )}
                  >
                    {formatDuration(track.durationMs)}
                  </td>
                  {showFavorite ? (
                    <td className={cn('text-center', favoritePadding)}>
                      <button
                        type="button"
                        aria-label={
                          favorited ? 'Remove from favorites' : 'Add to favorites'
                        }
                        className={cn(
                          'inline-flex size-7 items-center justify-center rounded-full text-caption transition hover:bg-muted hover:text-heading',
                          favorited && 'text-heading',
                        )}
                        onClick={(event) => {
                          event.stopPropagation()
                          toggleFavorite(track.id)
                        }}
                      >
                        <Heart
                          className="size-3.5"
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

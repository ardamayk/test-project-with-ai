import { ListMusic } from 'lucide-react'
import { usePlayback } from '../playback/PlaybackProvider'
import { useLayout } from './LayoutProvider'
import { PanelCollapseButton } from './PanelCollapseButton'
import { AlbumArt } from './AlbumArt'
import { getQueuePanel } from '../widgets/layout-utils'
import { cn } from '../lib/utils'

function formatDuration(ms: number): string {
  if (!ms || ms < 0) return '0:00'
  const total = Math.floor(ms / 1000)
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${s.toString().padStart(2, '0')}`
}

export function QueuePanel() {
  const { preferences, togglePanel } = useLayout()
  const panelSide = getQueuePanel(preferences.layout.sidebarPosition)
  const isCollapsed = preferences.layout.collapsed[panelSide]

  const { queue, currentTrack, playQueueIndex, removeFromQueue, clearQueue, getAlbumCoverUrl } =
    usePlayback()

  if (isCollapsed) {
    return (
      <div className="flex h-full flex-col items-center bg-queue text-queue-foreground">
        <div className="flex w-full justify-center px-1 pt-2">
          <PanelCollapseButton
            edge={panelSide}
            collapsed
            onToggle={() => togglePanel(panelSide)}
          />
        </div>
        <div
          className="relative mt-3 flex size-9 items-center justify-center rounded-md text-caption"
          title={`Queue${queue.length > 0 ? ` (${queue.length})` : ''}`}
        >
          <ListMusic className="size-4" />
          {queue.length > 0 ? (
            <span className="absolute -top-1 -right-1 flex size-4 items-center justify-center rounded-full bg-primary font-medium text-[0.625rem] text-primary-foreground">
              {queue.length > 9 ? '9+' : queue.length}
            </span>
          ) : null}
        </div>
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col">
      <div
        className={cn(
          'flex items-center justify-between gap-2 border-border border-b px-3 py-2',
          panelSide === 'left' && 'flex-row-reverse',
        )}
      >
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <h2 className="font-semibold text-base">Queue</h2>
          {queue.length > 0 ? (
            <button
              type="button"
              className="text-caption text-xs hover:text-foreground"
              onClick={() => void clearQueue()}
            >
              Clear all
            </button>
          ) : null}
        </div>
        <PanelCollapseButton
          edge={panelSide}
          collapsed={false}
          onToggle={() => togglePanel(panelSide)}
        />
      </div>
      <div className="flex-1 overflow-y-auto p-3">
        {queue.length === 0 ? (
          <p className="text-foreground text-sm">Queue is empty</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {queue.map((item, index) => (
              <li
                key={item.id}
                className="flex items-center gap-2 rounded-md px-2 py-2 hover:bg-muted/50"
              >
                <AlbumArt
                  coverUrl={getAlbumCoverUrl(item.track.albumId)}
                  title={item.track.title}
                  className="size-8 shrink-0 rounded text-xs"
                />
                <button
                  type="button"
                  className="min-w-0 flex-1 text-left"
                  onClick={() => void playQueueIndex(index)}
                >
                  <p
                    className={
                      item.track.id === currentTrack?.id
                        ? 'truncate font-medium text-heading text-sm'
                        : 'truncate text-foreground text-sm'
                    }
                  >
                    {item.track.title}
                  </p>
                  <p className="truncate text-foreground text-xs">
                    {item.track.artistName}
                  </p>
                </button>
                <span className="shrink-0 text-caption text-xs tabular-nums">
                  {formatDuration(item.track.durationMs)}
                </span>
                <button
                  type="button"
                  className="shrink-0 text-caption text-xs hover:text-foreground"
                  onClick={() => void removeFromQueue(item.id)}
                  aria-label="Remove from queue"
                >
                  ×
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
      <div className="border-border border-t p-3">
        <div className="rounded-lg border border-dashed border-border bg-muted/30 p-3">
          <p className="font-medium text-heading text-xs">Smart suggestion</p>
          <p className="mt-1 text-caption text-xs">
            Based on your listening — coming soon.
          </p>
        </div>
      </div>
    </div>
  )
}

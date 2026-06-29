import { usePlayback } from '../playback/PlaybackProvider'
import { cn } from '../lib/utils'

export function MiniPlayer() {
  const { currentTrack, isPlaying, togglePlay } = usePlayback()

  return (
    <button
      type="button"
      className={cn(
        'mx-3 mb-3 flex w-[calc(100%-1.5rem)] items-center gap-2 rounded-lg border border-border bg-card p-2 text-left transition hover:bg-muted/50',
        !currentTrack && 'opacity-70',
      )}
      onClick={togglePlay}
      disabled={!currentTrack}
    >
      <div className="flex size-10 shrink-0 items-center justify-center rounded bg-muted font-semibold text-xs uppercase">
        {currentTrack?.title?.slice(0, 1) ?? '♪'}
      </div>
      <div className="min-w-0 flex-1">
        <p className="truncate font-medium text-sm">
          {currentTrack?.title ?? 'Nothing playing'}
        </p>
        <p className="truncate text-muted-foreground text-xs">
          {currentTrack?.artistName ?? 'Select a track'}
        </p>
      </div>
      <span className="text-muted-foreground text-xs">
        {isPlaying ? '❚❚' : '▶'}
      </span>
    </button>
  )
}

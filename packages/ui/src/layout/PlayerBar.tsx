import {
  ChevronLeft,
  ChevronRight,
  Heart,
  ListMusic,
  Pause,
  Play,
  Repeat,
  Shuffle,
  SkipBack,
  SkipForward,
  Volume2,
} from 'lucide-react'
import { usePlayback } from '../playback/PlaybackProvider'
import { useLayout } from './LayoutProvider'
import { getQueuePanel } from '../widgets/layout-utils'
import { cn } from '../lib/utils'

function formatTime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '0:00'
  const m = Math.floor(seconds / 60)
  const s = Math.floor(seconds % 60)
  return `${m}:${s.toString().padStart(2, '0')}`
}

export function PlayerBar() {
  const { preferences, togglePanel } = useLayout()
  const queuePanelSide = getQueuePanel(preferences.layout.sidebarPosition)
  const {
    currentTrack,
    isPlaying,
    currentTime,
    duration,
    volume,
    togglePlay,
    seek,
    setVolume,
    playQueueIndex,
    queue,
  } = usePlayback()

  const currentIndex = queue.findIndex(
    (item) => item.track.id === currentTrack?.id,
  )

  const handleSeek = (value: number) => {
    if (duration > 0) {
      seek(value * duration)
    }
  }

  return (
    <footer className="border-border border-t bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/80">
      <div className="mx-auto flex max-w-screen-2xl items-center gap-4 px-4 py-3">
        <div className="flex min-w-0 flex-1 items-center gap-3">
          <div className="flex size-12 shrink-0 items-center justify-center rounded-md bg-muted font-semibold text-sm uppercase">
            {currentTrack?.title?.slice(0, 1) ?? '♪'}
          </div>
          <div className="min-w-0">
            <p className="truncate font-medium text-sm">
              {currentTrack?.title ?? 'Nothing playing'}
            </p>
            <p className="truncate text-muted-foreground text-xs">
              {currentTrack?.artistName ?? 'Select a track'}
            </p>
          </div>
          <button
            type="button"
            className="text-muted-foreground hover:text-primary"
            aria-label="Favorite"
            disabled={!currentTrack}
          >
            <Heart className="size-4" />
          </button>
        </div>

        <div className="flex flex-col items-center gap-1">
          <div className="flex items-center gap-1">
            <button
              type="button"
              className="inline-flex size-8 items-center justify-center rounded-full text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-40"
              aria-label="Shuffle"
              disabled={!currentTrack}
            >
              <Shuffle className="size-4" />
            </button>
            <button
              type="button"
              className="inline-flex size-8 items-center justify-center rounded-full hover:bg-muted disabled:opacity-40"
              onClick={() => {
                if (currentIndex > 0) {
                  void playQueueIndex(currentIndex - 1)
                }
              }}
              disabled={currentIndex <= 0}
              aria-label="Previous"
            >
              <SkipBack className="size-4" />
            </button>
            <button
              type="button"
              className={cn(
                'inline-flex size-10 items-center justify-center rounded-full bg-primary text-primary-foreground hover:opacity-90 disabled:opacity-50',
              )}
              onClick={togglePlay}
              disabled={!currentTrack}
              aria-label={isPlaying ? 'Pause' : 'Play'}
            >
              {isPlaying ? (
                <Pause className="size-4" />
              ) : (
                <Play className="size-4" />
              )}
            </button>
            <button
              type="button"
              className="inline-flex size-8 items-center justify-center rounded-full hover:bg-muted disabled:opacity-40"
              onClick={() => {
                if (currentIndex >= 0 && currentIndex < queue.length - 1) {
                  void playQueueIndex(currentIndex + 1)
                }
              }}
              disabled={currentIndex < 0 || currentIndex >= queue.length - 1}
              aria-label="Next"
            >
              <SkipForward className="size-4" />
            </button>
            <button
              type="button"
              className="inline-flex size-8 items-center justify-center rounded-full text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-40"
              aria-label="Repeat"
              disabled={!currentTrack}
            >
              <Repeat className="size-4" />
            </button>
          </div>
          <div className="flex items-center gap-2 text-xs tabular-nums">
            <span>{formatTime(currentTime)}</span>
            <input
              type="range"
              min={0}
              max={1}
              step={0.001}
              value={duration > 0 ? currentTime / duration : 0}
              onChange={(e) => handleSeek(Number(e.target.value))}
              className="w-40 accent-primary"
              disabled={!currentTrack}
              aria-label="Seek"
            />
            <span>{formatTime(duration)}</span>
          </div>
        </div>

        <div className="flex flex-1 items-center justify-end gap-2">
          <button
            type="button"
            className="text-muted-foreground hover:text-foreground"
            onClick={() => togglePanel(queuePanelSide)}
            aria-label="Toggle queue panel"
          >
            <ListMusic className="size-4" />
          </button>
          <Volume2 className="size-4 text-muted-foreground" />
          <input
            type="range"
            min={0}
            max={1}
            step={0.01}
            value={volume}
            onChange={(e) => setVolume(Number(e.target.value))}
            className="w-24 accent-primary"
            aria-label="Volume"
          />
        </div>
      </div>
    </footer>
  )
}

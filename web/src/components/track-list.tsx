import type { Track } from '@repo/api-client'
import { usePlayback } from '@repo/ui'

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
}: {
  tracks: Track[]
  albumId?: string
}) {
  const { playTrack, currentTrack } = usePlayback()

  const handlePlay = (track: Track) => {
    const queueTrackIds = tracks.map((t) => t.id)
    void playTrack(track.id, queueTrackIds)
  }

  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="border-border border-b text-left text-muted-foreground text-xs">
          <th className="px-2 py-2 font-medium">#</th>
          <th className="px-2 py-2 font-medium">Title</th>
          <th className="px-2 py-2 text-right font-medium">Duration</th>
        </tr>
      </thead>
      <tbody>
        {tracks.map((track, index) => (
          <tr
            key={track.id}
            className="cursor-pointer border-border/50 border-b hover:bg-muted/50"
            onClick={() => handlePlay(track)}
          >
            <td className="px-2 py-2 text-muted-foreground tabular-nums">
              {track.trackNo ?? index + 1}
            </td>
            <td className="px-2 py-2">
              <span
                className={
                  currentTrack?.id === track.id ? 'font-medium text-primary' : ''
                }
              >
                {track.title}
              </span>
              {!albumId && (
                <span className="block text-muted-foreground text-xs">
                  {track.artistName}
                </span>
              )}
            </td>
            <td className="px-2 py-2 text-right text-muted-foreground tabular-nums">
              {formatDuration(track.durationMs)}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

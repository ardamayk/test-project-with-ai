import { usePlayback } from '@repo/ui'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { invalidateLibraryCache } from '#/lib/invalidate-library-cache'
import { apiClient } from '#/lib/api'

async function syncPlaybackAfterDelete(
  playback: ReturnType<typeof usePlayback>,
  trackId?: string,
) {
  await playback.refreshQueue()

  if (trackId && playback.currentTrack?.id === trackId) {
    await playback.clearQueue()
  }
}

export function useDeleteAlbum() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const playback = usePlayback()

  return useMutation({
    mutationFn: (albumId: string) => apiClient.deleteAlbum(albumId),
    onSuccess: async (_result, albumId) => {
      const deletedTrackIds = queryClient
        .getQueryData<{ tracks: { id: string }[] }>(
          ['library', 'album', albumId],
        )
        ?.tracks.map((track) => track.id)

      if (
        playback.currentTrack &&
        deletedTrackIds?.includes(playback.currentTrack.id)
      ) {
        await playback.clearQueue()
      } else {
        await playback.refreshQueue()
      }

      await invalidateLibraryCache(queryClient, { albumId })

      if (window.location.pathname.includes(albumId)) {
        void navigate({ to: '/library/albums' })
      }
    },
  })
}

export function useDeleteTrack() {
  const queryClient = useQueryClient()
  const playback = usePlayback()

  return useMutation({
    mutationFn: (trackId: string) => apiClient.deleteTrack(trackId),
    onSuccess: async (_result, trackId) => {
      await syncPlaybackAfterDelete(playback, trackId)
      await invalidateLibraryCache(queryClient, { trackId })
    },
  })
}

export function confirmDelete(message: string): boolean {
  return window.confirm(message)
}

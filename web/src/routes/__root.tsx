import { Outlet, createRootRouteWithContext } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { QueryClient } from '@tanstack/react-query'
import {
  AppShell,
  defaultPreferences,
  LayoutProvider,
  PlaybackProvider,
  PlayerBar,
  SidebarNav,
  type PlaybackApi,
} from '@repo/ui'
import { apiClient } from '#/lib/api'
import { ThemeSync } from '#/components/theme-sync'
import { useLibraryScanSync } from '#/hooks/use-library-scan-sync'

const playbackApi: PlaybackApi = {
  getQueue: () => apiClient.getPlaybackQueue(),
  replaceQueue: (trackIds) => apiClient.replacePlaybackQueue(trackIds),
  appendQueueItem: (trackId) => apiClient.appendPlaybackQueueItem(trackId),
  removeQueueItem: (itemId) => apiClient.removePlaybackQueueItem(itemId),
  getStreamUrl: (trackId) => apiClient.getTrackStreamUrl(trackId),
  getAlbumCoverUrl: (albumId) => apiClient.getAlbumCoverUrl(albumId),
}

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  component: RootLayout,
})

function RootLayout() {
  const queryClient = useQueryClient()
  useLibraryScanSync()
  const preferences = useQuery({
    queryKey: ['preferences'],
    queryFn: () => apiClient.getPreferences(),
  })

  const patchPreferences = useMutation({
    mutationFn: apiClient.patchPreferences,
    onSuccess: (data) => {
      queryClient.setQueryData(['preferences'], data)
    },
  })

  if (preferences.isLoading) {
    return <div className="p-8">Loading…</div>
  }

  const initial = preferences.data ?? defaultPreferences

  return (
    <LayoutProvider
      initialPreferences={initial}
      onPreferencesChange={(prefs) => {
        patchPreferences.mutate(prefs)
      }}
    >
      <div className="flex h-full min-h-0 flex-1 flex-col overflow-hidden">
        <ThemeSync />
        <PlaybackProvider api={playbackApi}>
          <AppShell sidebar={<SidebarNav />} bottom={<PlayerBar />}>
            <Outlet />
          </AppShell>
        </PlaybackProvider>
      </div>
    </LayoutProvider>
  )
}

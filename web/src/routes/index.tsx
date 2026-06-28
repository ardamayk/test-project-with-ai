import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { AppShell, LayoutProvider } from '@repo/ui'
import { apiClient } from '#/lib/api'
import { Button } from '#/components/ui/button'

export const Route = createFileRoute('/')({
  component: HomePage,
})

function HomePage() {
  const queryClient = useQueryClient()
  const health = useQuery({
    queryKey: ['health'],
    queryFn: () => apiClient.getHealth(),
  })
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
    return <div className="p-8">Loading preferences…</div>
  }

  const initial = preferences.data ?? {
    theme: 'system' as const,
    layout: {
      sidebarPosition: 'left' as const,
      panels: { left: ['now-playing', 'queue'], right: ['discover'] },
      collapsed: { left: false, right: true },
    },
  }

  return (
    <LayoutProvider
      initialPreferences={initial}
      onPreferencesChange={(prefs) => {
        patchPreferences.mutate(prefs)
      }}
    >
      <AppShell>
        <header className="flex items-center justify-between border-border border-b px-6 py-4">
          <div>
            <h1 className="font-semibold text-xl">Navidrome Replacement</h1>
            <p className="text-muted-foreground text-sm">
              API: {health.data?.status ?? '…'} v{health.data?.version ?? '…'}
            </p>
          </div>
          <LayoutControls />
        </header>
        <section className="p-6">
          <p className="text-muted-foreground">
            Modular widget shell. Toggle panels or move sidebar — preferences sync
            to server.
          </p>
        </section>
      </AppShell>
    </LayoutProvider>
  )
}

function LayoutControls() {
  return (
    <div className="flex gap-2">
      <SidebarToggle side="left" />
      <SidebarToggle side="right" />
    </div>
  )
}

function SidebarToggle({ side }: { side: 'left' | 'right' }) {
  const queryClient = useQueryClient()
  const preferences = useQuery({
    queryKey: ['preferences'],
    queryFn: () => apiClient.getPreferences(),
  })

  const patch = useMutation({
    mutationFn: apiClient.patchPreferences,
    onSuccess: (data) => queryClient.setQueryData(['preferences'], data),
  })

  const moveSidebar = () => {
    const layout = preferences.data?.layout
    if (!layout) return
    patch.mutate({
      layout: { ...layout, sidebarPosition: side },
    })
  }

  return (
    <Button type="button" variant="outline" size="sm" onClick={moveSidebar}>
      Sidebar {side}
    </Button>
  )
}

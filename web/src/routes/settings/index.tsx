import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import type { ThemePreferences } from '@repo/api-client'
import { useLayout } from '@repo/ui'
import { Button } from '#/components/ui/button'
import { apiClient } from '#/lib/api'

const themePresets: ThemePreferences['preset'][] = ['earthly', 'tokyo-night']
const themeModes: ThemePreferences['mode'][] = ['light', 'dark', 'system']

export const Route = createFileRoute('/settings/')({
  component: SettingsPage,
})

function SettingsPage() {
  const queryClient = useQueryClient()
  const { preferences, setPreferences } = useLayout()

  const patchPreferences = useMutation({
    mutationFn: apiClient.patchPreferences,
    onSuccess: (data) => {
      queryClient.setQueryData(['preferences'], data)
    },
  })

  const health = useQuery({
    queryKey: ['health'],
    queryFn: () => apiClient.getHealth(),
  })

  const saveTheme = (theme: Partial<ThemePreferences>) => {
    const next = { ...preferences.theme, ...theme }
    setPreferences({ theme: next })
    patchPreferences.mutate({ theme: next })
  }

  return (
    <div className="p-6">
      <header className="mb-6">
        <h1 className="font-semibold text-2xl">Settings</h1>
        <p className="text-muted-foreground text-sm">
          Appearance · API {health.data?.status ?? '…'} v
          {health.data?.version ?? '…'}
        </p>
      </header>

      <section className="mb-8 flex flex-col gap-4">
        <h2 className="font-medium text-sm">Theme preset</h2>
        <div className="flex flex-wrap gap-2">
          {themePresets.map((preset) => (
            <Button
              key={preset}
              type="button"
              size="sm"
              variant={
                preferences.theme.preset === preset ? 'default' : 'outline'
              }
              onClick={() => saveTheme({ preset })}
            >
              {preset === 'earthly' ? 'Earthly' : 'Tokyo Night'}
            </Button>
          ))}
        </div>
      </section>

      <section className="flex flex-col gap-4">
        <h2 className="font-medium text-sm">Appearance</h2>
        <div className="flex flex-wrap gap-2">
          {themeModes.map((mode) => (
            <Button
              key={mode}
              type="button"
              size="sm"
              variant={preferences.theme.mode === mode ? 'default' : 'outline'}
              onClick={() => saveTheme({ mode })}
            >
              {mode}
            </Button>
          ))}
        </div>
      </section>
    </div>
  )
}

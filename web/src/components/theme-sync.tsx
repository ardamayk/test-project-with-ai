import { useLayout, ThemeProvider } from '@repo/ui'

export function ThemeSync() {
  const { preferences } = useLayout()
  return <ThemeProvider theme={preferences.theme} />
}

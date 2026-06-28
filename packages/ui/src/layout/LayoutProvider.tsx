import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import type { LayoutPreferences, UserPreferences } from '@repo/api-client'
import { defaultLayout } from '../widgets/types'

type LayoutContextValue = {
  preferences: UserPreferences
  setPreferences: (next: Partial<UserPreferences>) => void
  togglePanel: (side: 'left' | 'right') => void
  moveSidebar: (position: 'left' | 'right') => void
}

const LayoutContext = createContext<LayoutContextValue | null>(null)

const defaultPreferences: UserPreferences = {
  theme: 'system',
  layout: defaultLayout,
}

export function LayoutProvider({
  children,
  initialPreferences = defaultPreferences,
  onPreferencesChange,
}: {
  children: ReactNode
  initialPreferences?: UserPreferences
  onPreferencesChange?: (prefs: UserPreferences) => void
}) {
  const [preferences, setPreferencesState] =
    useState<UserPreferences>(initialPreferences)

  const commit = useCallback(
    (next: UserPreferences) => {
      setPreferencesState(next)
      onPreferencesChange?.(next)
    },
    [onPreferencesChange],
  )

  const setPreferences = useCallback(
    (patch: Partial<UserPreferences>) => {
      commit({
        ...preferences,
        ...patch,
        layout: patch.layout
          ? { ...preferences.layout, ...patch.layout }
          : preferences.layout,
      })
    },
    [commit, preferences],
  )

  const togglePanel = useCallback(
    (side: 'left' | 'right') => {
      const layout: LayoutPreferences = {
        ...preferences.layout,
        collapsed: {
          ...preferences.layout.collapsed,
          [side]: !preferences.layout.collapsed[side],
        },
      }
      commit({ ...preferences, layout })
    },
    [commit, preferences],
  )

  const moveSidebar = useCallback(
    (position: 'left' | 'right') => {
      commit({
        ...preferences,
        layout: { ...preferences.layout, sidebarPosition: position },
      })
    },
    [commit, preferences],
  )

  const value = useMemo(
    () => ({
      preferences,
      setPreferences,
      togglePanel,
      moveSidebar,
    }),
    [preferences, setPreferences, togglePanel, moveSidebar],
  )

  return (
    <LayoutContext.Provider value={value}>{children}</LayoutContext.Provider>
  )
}

export function useLayout() {
  const ctx = useContext(LayoutContext)
  if (!ctx) {
    throw new Error('useLayout must be used within LayoutProvider')
  }
  return ctx
}

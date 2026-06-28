import type { LayoutPreferences } from '@repo/api-client'

export type WidgetDefinition = {
  id: string
  title: string
  component: React.ComponentType
}

export type WidgetPlacement = 'left' | 'right' | 'main'

export const defaultLayout: LayoutPreferences = {
  sidebarPosition: 'left',
  panels: {
    left: ['now-playing', 'queue'],
    right: ['discover'],
  },
  collapsed: {
    left: false,
    right: true,
  },
}

import type { ReactNode } from 'react'
import { Panel, Slot } from './Slot'
import { useLayout } from './LayoutProvider'

export function AppShell({ children }: { children?: ReactNode }) {
  const { preferences, togglePanel } = useLayout()
  const { layout } = preferences
  const isLeftPrimary = layout.sidebarPosition === 'left'

  const leftPanel = (
    <Panel
      side="left"
      collapsed={layout.collapsed.left}
      onToggle={() => togglePanel('left')}
    >
      <Slot widgetIds={layout.panels.left} />
    </Panel>
  )

  const rightPanel = (
    <Panel
      side="right"
      collapsed={layout.collapsed.right}
      onToggle={() => togglePanel('right')}
    >
      <Slot widgetIds={layout.panels.right} />
    </Panel>
  )

  return (
    <div className="flex min-h-screen bg-background text-foreground">
      {isLeftPrimary ? leftPanel : rightPanel}
      <main className="flex min-w-0 flex-1 flex-col">{children}</main>
      {isLeftPrimary ? rightPanel : leftPanel}
    </div>
  )
}

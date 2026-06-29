import type { ReactNode } from 'react'
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from '../components/resizable'
import { useLayout } from './LayoutProvider'
import { QueuePanel } from './QueuePanel'
import { WidgetDock, WidgetDndProvider } from './WidgetDock'
import {
  clampPanelSizes,
  getNavPanel,
  getQueuePanel,
  MINI_PANEL_MAX,
} from '../widgets/layout-utils'

/** Panel size strings — react-resizable-panels v4 treats numbers as px, strings as %. */
const MIN_LEFT = '15'
const MAX_LEFT = '45'
const MIN_RIGHT = '18'
const MAX_RIGHT = '50'
const MIN_MAIN = '25'
const MIN_MINI = '4'
const MAX_MINI = String(MINI_PANEL_MAX)

function pct(n: number): `${number}%` {
  return `${n}%`
}

export function AppShell({
  children,
  sidebar,
  bottom,
}: {
  children?: ReactNode
  sidebar?: ReactNode
  bottom?: ReactNode
}) {
  const { preferences, setPanelSizes } = useLayout()
  const { layout } = preferences
  const sizes = clampPanelSizes(layout.sizes ?? [22, 50, 28], layout.collapsed)
  const panelLayout = { left: sizes[0], main: sizes[1], right: sizes[2] }
  const isLeftPrimary = layout.sidebarPosition === 'left'
  const navPanel = getNavPanel(layout.sidebarPosition)
  const queuePanel = getQueuePanel(layout.sidebarPosition)
  const navCollapsed = layout.collapsed[navPanel]
  const queueCollapsed = layout.collapsed[queuePanel]

  const layoutToSizes = (next: Record<string, number>): [number, number, number] =>
    clampPanelSizes(
      [next.left ?? sizes[0], next.main ?? sizes[1], next.right ?? sizes[2]],
      layout.collapsed,
    )

  const navWidgetPanel = navPanel === 'left' ? 'left' : 'right'
  const queueWidgetPanel = queuePanel === 'left' ? 'left' : 'right'

  const navColumn = (
    <div className="flex h-full w-full flex-col overflow-hidden bg-sidebar">
      {sidebar}
      {!navCollapsed ? (
        <div className="min-h-0 flex-1 overflow-y-auto border-sidebar-border border-t">
          <WidgetDock panel={navWidgetPanel} />
        </div>
      ) : null}
    </div>
  )

  const queueColumn = (
    <div className="flex h-full w-full flex-col overflow-hidden bg-background">
      <div className="min-h-0 flex-[2] overflow-hidden">
        <QueuePanel />
      </div>
      {!queueCollapsed ? (
        <div className="min-h-0 flex-1 overflow-y-auto border-border border-t">
          <WidgetDock panel={queueWidgetPanel} />
        </div>
      ) : null}
    </div>
  )

  const leftMin = layout.collapsed.left ? MIN_MINI : MIN_LEFT
  const leftMax = layout.collapsed.left ? MAX_MINI : MAX_LEFT
  const rightMin = layout.collapsed.right ? MIN_MINI : MIN_RIGHT
  const rightMax = layout.collapsed.right ? MAX_MINI : MAX_RIGHT

  return (
    <WidgetDndProvider>
      <div className="flex h-full min-h-0 w-full flex-1 flex-col overflow-hidden bg-background text-foreground">
        <ResizablePanelGroup
          key={`shell-${layout.collapsed.left}-${layout.collapsed.right}-${sizes.join('-')}`}
          id="earthly-shell"
          orientation="horizontal"
          className="h-full min-h-0 flex-1"
          defaultLayout={panelLayout}
          onLayoutChanged={(next, meta) => {
            if (meta && 'isUserInteraction' in meta && !meta.isUserInteraction) {
              return
            }
            setPanelSizes(layoutToSizes(next))
          }}
        >
          <ResizablePanel
            id="left"
            defaultSize={pct(sizes[0])}
            minSize={leftMin}
            maxSize={leftMax}
            className="min-w-0"
          >
            {isLeftPrimary ? navColumn : queueColumn}
          </ResizablePanel>
          <ResizableHandle withHandle />
          <ResizablePanel
            id="main"
            defaultSize={pct(sizes[1])}
            minSize={MIN_MAIN}
            className="min-w-0"
          >
            <main className="flex h-full min-w-0 flex-col overflow-auto bg-background">
              {children}
            </main>
          </ResizablePanel>
          <ResizableHandle withHandle />
          <ResizablePanel
            id="right"
            defaultSize={pct(sizes[2])}
            minSize={rightMin}
            maxSize={rightMax}
            className="min-w-0"
          >
            {isLeftPrimary ? queueColumn : navColumn}
          </ResizablePanel>
        </ResizablePanelGroup>
        {bottom ? <div className="shrink-0 border-border border-t">{bottom}</div> : null}
      </div>
    </WidgetDndProvider>
  )
}

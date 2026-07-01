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
  const hideQueuePanel = queueCollapsed

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
  const fixedNavColumn = (
    <aside className="h-full w-max shrink-0 overflow-hidden">{navColumn}</aside>
  )

  const queueColumn = (
    <div className="flex h-full w-full flex-col overflow-hidden bg-queue text-queue-foreground">
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

  const leftMin = layout.collapsed.left
    ? queuePanel === 'left'
      ? MIN_LEFT
      : MIN_MINI
    : MIN_LEFT
  const leftMax = layout.collapsed.left
    ? queuePanel === 'left'
      ? MAX_LEFT
      : MAX_MINI
    : MAX_LEFT
  const rightMin = layout.collapsed.right
    ? queuePanel === 'right'
      ? MIN_RIGHT
      : MIN_MINI
    : MIN_RIGHT
  const rightMax = layout.collapsed.right
    ? queuePanel === 'right'
      ? MAX_RIGHT
      : MAX_MINI
    : MAX_RIGHT

  const leftVisible = !(hideQueuePanel && queuePanel === 'left') && navPanel !== 'left'
  const rightVisible = !(hideQueuePanel && queuePanel === 'right') && navPanel !== 'right'
  const fixedNavSize = panelLayout[navPanel]
  const resizableTotal = 100 - fixedNavSize
  const fromResizablePct = (size: number | undefined, fallback: number): number =>
    size === undefined ? fallback : Math.round((size / 100) * resizableTotal * 10) / 10
  const toResizablePct = (size: number): number =>
    resizableTotal > 0 ? Math.round((size / resizableTotal) * 1000) / 10 : size
  const layoutToSizes = (next: Record<string, number>): [number, number, number] =>
    clampPanelSizes(
      [
        navPanel === 'left'
          ? fixedNavSize
          : fromResizablePct(next.left, sizes[0]),
        fromResizablePct(next.main, sizes[1]),
        navPanel === 'right'
          ? fixedNavSize
          : fromResizablePct(next.right, sizes[2]),
      ],
      layout.collapsed,
    )
  const visibleResizableLayout = {
    ...(leftVisible ? { left: toResizablePct(panelLayout.left) } : {}),
    main:
      toResizablePct(panelLayout.main) +
      (!leftVisible && queuePanel === 'left' ? toResizablePct(panelLayout.left) : 0) +
      (!rightVisible && queuePanel === 'right' ? toResizablePct(panelLayout.right) : 0),
    ...(rightVisible ? { right: toResizablePct(panelLayout.right) } : {}),
  }

  return (
    <WidgetDndProvider>
      <div className="flex h-full min-h-0 w-full flex-1 flex-col overflow-hidden bg-background text-foreground">
        <div className="flex min-h-0 flex-1 overflow-hidden">
          {navPanel === 'left' ? fixedNavColumn : null}
          <ResizablePanelGroup
            key={`shell-${layout.collapsed.left}-${layout.collapsed.right}-${sizes.join('-')}`}
            id="earthly-shell"
            orientation="horizontal"
            className="h-full min-h-0 flex-1"
            defaultLayout={visibleResizableLayout}
            onLayoutChanged={(next, meta) => {
              if (meta && 'isUserInteraction' in meta && !meta.isUserInteraction) {
                return
              }
              setPanelSizes(layoutToSizes(next))
            }}
          >
            {leftVisible ? (
              <>
                <ResizablePanel
                  id="left"
                  defaultSize={pct(visibleResizableLayout.left ?? panelLayout.left)}
                  minSize={leftMin}
                  maxSize={leftMax}
                  className="min-w-0"
                >
                  {isLeftPrimary ? navColumn : queueColumn}
                </ResizablePanel>
                <ResizableHandle withHandle />
              </>
            ) : null}
            <ResizablePanel
              id="main"
              defaultSize={pct(visibleResizableLayout.main)}
              minSize={MIN_MAIN}
              className="min-w-0"
            >
              <main className="flex h-full min-w-0 flex-col overflow-auto bg-background">
                {children}
              </main>
            </ResizablePanel>
            {rightVisible ? (
              <>
                <ResizableHandle withHandle />
                <ResizablePanel
                  id="right"
                  defaultSize={pct(visibleResizableLayout.right ?? panelLayout.right)}
                  minSize={rightMin}
                  maxSize={rightMax}
                  className="min-w-0"
                >
                  {isLeftPrimary ? queueColumn : navColumn}
                </ResizablePanel>
              </>
            ) : null}
          </ResizablePanelGroup>
          {navPanel === 'right' ? fixedNavColumn : null}
        </div>
        {bottom ? <div className="shrink-0 border-border border-t">{bottom}</div> : null}
      </div>
    </WidgetDndProvider>
  )
}

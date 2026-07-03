import type { ReactNode } from 'react'
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from '../components/resizable'
import { useLayout } from './LayoutProvider'
import { QueuePanel } from './QueuePanel'
import { WidgetDock, WidgetDndProvider } from './WidgetDock'
import { deriveShellLayout } from '../widgets/layout-utils'

/** Panel size strings — react-resizable-panels v4 treats numbers as px, strings as %. */
const MIN_MAIN = '25'

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
  const {
    sizes,
    panelLayout,
    navPanel,
    queuePanel,
    navCollapsed,
    queueCollapsed,
    leftVisible,
    rightVisible,
    leftMin,
    leftMax,
    rightMin,
    rightMax,
    visibleResizableLayout,
    toPanelSizes,
  } = deriveShellLayout(layout)
  const isLeftPrimary = layout.sidebarPosition === 'left'

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
    <aside className="h-full w-fit min-w-max max-w-[min(22rem,45vw)] shrink-0 overflow-hidden">
      {navColumn}
    </aside>
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
              setPanelSizes(toPanelSizes(next))
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

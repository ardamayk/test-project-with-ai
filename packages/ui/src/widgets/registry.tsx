import type { ComponentType } from 'react'
import type { WidgetDefinition } from './types'

function PlaceholderWidget({ title }: { title: string }) {
  return (
    <div className="rounded-lg border border-dashed border-border p-4 text-sm text-muted-foreground">
      {title} (placeholder)
    </div>
  )
}

function NowPlayingWidget() {
  return <PlaceholderWidget title="Now Playing" />
}

function QueueWidget() {
  return <PlaceholderWidget title="Queue" />
}

function DiscoverWidget() {
  return <PlaceholderWidget title="Discover" />
}

const widgetComponents: Record<string, ComponentType> = {
  'now-playing': NowPlayingWidget,
  queue: QueueWidget,
  discover: DiscoverWidget,
}

export const widgetRegistry: WidgetDefinition[] = [
  { id: 'now-playing', title: 'Now Playing', component: NowPlayingWidget },
  { id: 'queue', title: 'Queue', component: QueueWidget },
  { id: 'discover', title: 'Discover', component: DiscoverWidget },
]

export function getWidgetComponent(id: string): ComponentType | undefined {
  return widgetComponents[id]
}

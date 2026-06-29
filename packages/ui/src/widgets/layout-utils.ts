import type { LayoutPreferences } from '@repo/api-client'
import { defaultLayout } from './types'

const MIN_LEFT = 15
const MAX_LEFT = 45
const MIN_RIGHT = 18
const MAX_RIGHT = 50
const MIN_MAIN = 25

export const MINI_PANEL_SIZE = 5
export const MINI_PANEL_MAX = 12

export function getNavPanel(
  sidebarPosition: LayoutPreferences['sidebarPosition'],
): 'left' | 'right' {
  return sidebarPosition === 'left' ? 'left' : 'right'
}

export function getQueuePanel(
  sidebarPosition: LayoutPreferences['sidebarPosition'],
): 'left' | 'right' {
  return sidebarPosition === 'left' ? 'right' : 'left'
}

/** Enforce readable minimums; allow wide side panels up to generous maxima. */
export function clampPanelSizes(
  sizes: number[],
  collapsed: { left?: boolean; right?: boolean } = {},
): [number, number, number] {
  const fallback = defaultLayout.sizes ?? [22, 50, 28]
  let left = sizes[0] ?? fallback[0]
  let right = sizes[2] ?? fallback[2]

  if (collapsed.left) {
    left = Math.max(4, Math.min(MINI_PANEL_MAX, left))
  } else {
    left = Math.max(MIN_LEFT, Math.min(MAX_LEFT, left))
  }

  if (collapsed.right) {
    right = Math.max(4, Math.min(MINI_PANEL_MAX, right))
  } else {
    right = Math.max(MIN_RIGHT, Math.min(MAX_RIGHT, right))
  }

  let main = 100 - left - right
  if (main < MIN_MAIN) {
    const deficit = MIN_MAIN - main
    const sideSum = left + right
    left -= (left / sideSum) * deficit
    right -= (right / sideSum) * deficit
    main = MIN_MAIN
  }

  return [
    Math.round(left * 10) / 10,
    Math.round(main * 10) / 10,
    Math.round(right * 10) / 10,
  ]
}

export function normalizeLayout(layout: LayoutPreferences): LayoutPreferences {
  const base = defaultLayout
  const rawSizes =
    layout.sizes?.length === 3 ? layout.sizes : (base.sizes ?? [22, 50, 28])
  const collapsed = {
    left: layout.collapsed?.left ?? base.collapsed.left,
    right: layout.collapsed?.right ?? base.collapsed.right,
  }

  return {
    sidebarPosition: layout.sidebarPosition ?? base.sidebarPosition,
    panels: {
      left: layout.panels?.left ?? base.panels.left,
      right: layout.panels?.right ?? base.panels.right,
    },
    collapsed,
    sizes: clampPanelSizes(rawSizes, collapsed),
  }
}

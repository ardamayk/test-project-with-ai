import { describe, expect, it } from 'vitest'
import { clampPanelSizes, normalizeLayout } from '../widgets/layout-utils'
import { defaultLayout } from '../widgets/types'

describe('layout utils', () => {
  it('fills missing sizes with defaults', () => {
    const result = normalizeLayout({
      sidebarPosition: 'left',
      panels: { left: ['now-playing'], right: ['discover'] },
      collapsed: { left: false, right: false },
    })
    expect(result.sizes).toEqual(defaultLayout.sizes)
  })

  it('clamps collapsed drag sizes back to readable minimums', () => {
    const result = clampPanelSizes([3, 94, 3])
    expect(result[0]).toBeGreaterThanOrEqual(15)
    expect(result[2]).toBeGreaterThanOrEqual(18)
    expect(result[0] + result[1] + result[2]).toBeCloseTo(100, 0)
  })

  it('allows mini widths when a panel is collapsed', () => {
    const result = clampPanelSizes([5, 70, 25], { left: true, right: false })
    expect(result[0]).toBe(5)
    expect(result[0] + result[1] + result[2]).toBeCloseTo(100, 0)
  })
})

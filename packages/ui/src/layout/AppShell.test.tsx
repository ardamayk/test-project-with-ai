import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AppShell } from './AppShell'
import { LayoutProvider } from './LayoutProvider'

describe('AppShell', () => {
  it('renders placeholder widgets', () => {
    render(
      <LayoutProvider>
        <AppShell>
          <div>Main content</div>
        </AppShell>
      </LayoutProvider>,
    )
    expect(screen.getByText('Main content')).toBeTruthy()
    expect(screen.getByText('Now Playing (placeholder)')).toBeTruthy()
  })
})

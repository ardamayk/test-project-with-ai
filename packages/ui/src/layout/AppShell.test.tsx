import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import {
  AppShell,
  LayoutProvider,
  PlaybackProvider,
  defaultPreferences,
} from '../index'

const mockPlaybackApi = {
  getQueue: async () => ({ items: [] }),
  replaceQueue: async () => ({ items: [] }),
  appendQueueItem: async () => ({ items: [] }),
  removeQueueItem: async () => ({ items: [] }),
  clearQueue: async () => ({ items: [] }),
  getStreamUrl: (id: string) => `/stream/${id}`,
}

describe('AppShell', () => {
  it('renders main content and widgets', () => {
    render(
      <LayoutProvider initialPreferences={defaultPreferences}>
        <PlaybackProvider api={mockPlaybackApi}>
          <AppShell>
            <div>Main content</div>
          </AppShell>
        </PlaybackProvider>
      </LayoutProvider>,
    )
    expect(screen.getByText('Main content')).toBeTruthy()
    expect(screen.getByText('Nothing playing')).toBeTruthy()
    expect(screen.getByText('Queue')).toBeTruthy()
  })
})

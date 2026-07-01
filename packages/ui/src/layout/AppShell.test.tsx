import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
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
  getAlbumCoverUrl: (id: string) => `/cover/${id}`,
  listPlaylists: async () => ({ items: [], total: 0 }),
  getPlaylist: async (playlistId: string) => ({
    id: playlistId,
    name: 'Playlist',
    isDefault: false,
    trackCount: 0,
    tracks: [],
  }),
  createPlaylist: async (name: string) => ({
    id: 'playlist-1',
    name,
    isDefault: false,
    trackCount: 0,
  }),
  addPlaylistTrack: async () => ({
    id: 'playlist-1',
    name: 'Playlist',
    isDefault: false,
    trackCount: 1,
    tracks: [],
  }),
  removePlaylistTrack: async () => ({
    id: 'playlist-1',
    name: 'Playlist',
    isDefault: false,
    trackCount: 0,
    tracks: [],
  }),
}

class AudioMock extends EventTarget {
  volume = 1
  pause = vi.fn()
  play = vi.fn(async () => {})
  removeAttribute = vi.fn()
}

describe('AppShell', () => {
  const originalAudio = globalThis.Audio

  beforeEach(() => {
    globalThis.Audio = AudioMock as unknown as typeof Audio
  })

  afterEach(() => {
    cleanup()
    globalThis.Audio = originalAudio
  })

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

  it('does not render a resize handle for the primary nav column', () => {
    const { container } = render(
      <LayoutProvider initialPreferences={defaultPreferences}>
        <PlaybackProvider api={mockPlaybackApi}>
          <AppShell>
            <div>Main content</div>
          </AppShell>
        </PlaybackProvider>
      </LayoutProvider>,
    )

    expect(container.querySelectorAll('[data-slot="resizable-handle"]')).toHaveLength(1)
  })

  it('hides the queue column when the queue panel is collapsed', () => {
    const collapsedQueuePreferences = {
      ...defaultPreferences,
      layout: {
        ...defaultPreferences.layout,
        collapsed: { left: false, right: true },
      },
    }

    render(
      <LayoutProvider initialPreferences={collapsedQueuePreferences}>
        <PlaybackProvider api={mockPlaybackApi}>
          <AppShell>
            <div>Main content</div>
          </AppShell>
        </PlaybackProvider>
      </LayoutProvider>,
    )

    expect(screen.queryByText('Queue')).toBeNull()
    expect(screen.queryByTitle('Queue')).toBeNull()
    expect(screen.getByText('Main content')).toBeTruthy()
  })
})

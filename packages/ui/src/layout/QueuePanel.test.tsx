import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PlaybackProvider, type PlaybackApi } from '../playback/PlaybackProvider'
import { defaultPreferences } from '../widgets/types'
import { LayoutProvider } from './LayoutProvider'
import { QueuePanel } from './QueuePanel'

const track = {
  id: 'track-1',
  title: 'Track 1',
  artistName: 'Artist',
  albumId: 'album-1',
  durationMs: 120000,
  format: 'opus',
}

function createApi(): PlaybackApi {
  return {
    getQueue: vi.fn(async () => ({
      items: [{ id: 'item-1', trackId: track.id, position: 0, track }],
    })),
    replaceQueue: vi.fn(async () => ({ items: [] })),
    appendQueueItem: vi.fn(async () => ({ items: [] })),
    removeQueueItem: vi.fn(async () => ({ items: [] })),
    getStreamUrl: (trackId) => `/stream/${trackId}`,
    getAlbumCoverUrl: (albumId) => `/cover/${albumId}`,
    getRadioStationStreamUrl: (stationId) => `/radio/${stationId}`,
    getRadioNowPlaying: vi.fn(async () => ({})),
    listPlaylists: vi.fn(async () => ({ items: [], total: 0 })),
    getPlaylist: vi.fn(async (playlistId: string) => ({
      id: playlistId,
      name: 'Playlist',
      isDefault: false,
      trackCount: 0,
      tracks: [],
    })),
    createPlaylist: vi.fn(async (name: string) => ({
      id: 'playlist-1',
      name,
      isDefault: false,
      trackCount: 0,
    })),
    addPlaylistTrack: vi.fn(async () => ({
      id: 'playlist-1',
      name: 'Playlist',
      isDefault: false,
      trackCount: 1,
      tracks: [track],
    })),
    removePlaylistTrack: vi.fn(async () => ({
      id: 'playlist-1',
      name: 'Playlist',
      isDefault: false,
      trackCount: 0,
      tracks: [],
    })),
  }
}

class AudioMock extends EventTarget {
  static instances: AudioMock[] = []

  currentTime = 0
  duration = 120
  paused = true
  src = ''
  volume = 1
  pause = vi.fn()
  play = vi.fn(async () => {
    this.paused = false
    this.dispatchEvent(new Event('play'))
  })
  removeAttribute = vi.fn()

  constructor() {
    super()
    AudioMock.instances.push(this)
  }
}

describe('QueuePanel', () => {
  const originalAudio = globalThis.Audio

  beforeEach(() => {
    AudioMock.instances = []
    globalThis.Audio = AudioMock as unknown as typeof Audio
  })

  afterEach(() => {
    cleanup()
    globalThis.Audio = originalAudio
  })

  it('plays a track when left-clicking the queue row', async () => {
    const { container } = render(
      <LayoutProvider initialPreferences={defaultPreferences}>
        <PlaybackProvider api={createApi()}>
          <QueuePanel />
        </PlaybackProvider>
      </LayoutProvider>,
    )

    const title = await screen.findByText('Track 1')
    const row = title.closest('li')

    await act(async () => {
      fireEvent.click(row as HTMLElement)
    })

    expect(AudioMock.instances[0]?.src).toBe('/stream/track-1')
    expect(AudioMock.instances[0]?.play).toHaveBeenCalledOnce()
  })
})

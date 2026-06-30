import { act, cleanup, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PlaybackProvider, usePlayback, type PlaybackApi } from './PlaybackProvider'

const track = {
  id: 'track-1',
  title: 'Track 1',
  artistName: 'Artist',
  albumId: 'album-1',
  durationMs: 120000,
  format: 'flac',
}

class AudioMock extends EventTarget {
  static instances: AudioMock[] = []

  currentTime = 0
  duration = 120
  paused = true
  src = ''
  volume = 1
  pause = vi.fn(() => {
    this.paused = true
    this.dispatchEvent(new Event('pause'))
  })
  play = vi.fn(async () => {
    this.paused = false
    this.dispatchEvent(new Event('play'))
  })
  removeAttribute = vi.fn((name: string) => {
    if (name === 'src') {
      this.src = ''
    }
  })

  constructor() {
    super()
    AudioMock.instances.push(this)
  }
}

function createApi(): PlaybackApi {
  return {
    getQueue: vi.fn(async () => ({
      items: [{ id: 'item-1', trackId: track.id, position: 0, track }],
    })),
    replaceQueue: vi.fn(async () => ({
      items: [{ id: 'item-1', trackId: track.id, position: 0, track }],
    })),
    appendQueueItem: vi.fn(async () => ({
      items: [{ id: 'item-1', trackId: track.id, position: 0, track }],
    })),
    removeQueueItem: vi.fn(async () => ({ items: [] })),
    getStreamUrl: (trackId) => `/stream/${trackId}`,
    getAlbumCoverUrl: (albumId) => `/cover/${albumId}`,
  }
}

function Harness({ children }: { children?: ReactNode }) {
  const playback = usePlayback()
  return (
    <div>
      <span data-testid="volume">{playback.volume}</span>
      <span data-testid="playing">{String(playback.isPlaying)}</span>
      <button type="button" onClick={() => playback.setVolume(0.3)}>
        Set volume
      </button>
      <button type="button" onClick={() => void playback.playTrack(track.id)}>
        Play
      </button>
      {children}
    </div>
  )
}

describe('PlaybackProvider', () => {
  const originalAudio = globalThis.Audio

  beforeEach(() => {
    AudioMock.instances = []
    globalThis.Audio = AudioMock as unknown as typeof Audio
  })

  afterEach(() => {
    cleanup()
    globalThis.Audio = originalAudio
  })

  it('updates volume without recreating the audio element', async () => {
    render(
      <PlaybackProvider api={createApi()}>
        <Harness />
      </PlaybackProvider>,
    )

    await screen.findByTestId('volume')
    expect(AudioMock.instances).toHaveLength(1)

    await act(async () => {
      screen.getByRole('button', { name: 'Set volume' }).click()
    })

    expect(screen.getByTestId('volume').textContent).toBe('0.3')
    expect(AudioMock.instances).toHaveLength(1)
    expect(AudioMock.instances[0]?.volume).toBe(0.3)
  })

  it('plays a queued track through the stream URL', async () => {
    render(
      <PlaybackProvider api={createApi()}>
        <Harness />
      </PlaybackProvider>,
    )

    await act(async () => {
      screen.getByRole('button', { name: 'Play' }).click()
    })

    expect(AudioMock.instances[0]?.src).toBe('/stream/track-1')
    expect(AudioMock.instances[0]?.play).toHaveBeenCalledOnce()
    expect(screen.getByTestId('playing').textContent).toBe('true')
  })
})

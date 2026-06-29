import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { TrackList } from './track-list'

vi.mock('@repo/ui', () => ({
  usePlayback: () => ({
    playTrack: vi.fn(),
    currentTrack: null,
  }),
}))

vi.mock('#/hooks/use-favorite-tracks', () => ({
  useFavoriteTracks: () => ({
    isFavorite: () => false,
    toggleFavorite: vi.fn(),
  }),
}))

vi.mock('#/hooks/use-delete-library', () => ({
  useDeleteTrack: () => ({ mutate: vi.fn(), isPending: false }),
  confirmDelete: () => false,
}))

const sampleTrack = {
  id: 't1',
  title: 'Welcome to New York',
  artistName: 'Taylor Swift',
  albumId: 'a1',
  durationMs: 212_000,
  format: 'flac',
  genre: 'Pop',
  bitDepth: 24,
  sampleRateHz: 96_000,
}

describe('TrackList', () => {
  it('renders compact metadata line', () => {
    render(
      <TrackList tracks={[sampleTrack]} albumId="a1" showMeta compact />,
    )
    expect(screen.getByText('Welcome to New York')).toBeTruthy()
    expect(screen.getByText('Pop · FLAC · 24-bit · 96 kHz')).toBeTruthy()
  })
})

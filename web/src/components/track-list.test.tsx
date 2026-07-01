import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { TrackList } from './track-list'

const toggleFavorite = vi.fn()
let favorite = false

vi.mock('@repo/ui', () => ({
  usePlayback: () => ({
    playTrack: vi.fn(),
    currentTrack: null,
  }),
}))

vi.mock('#/hooks/use-favorite-tracks', () => ({
  useFavoriteTracks: () => ({
    isFavorite: () => favorite,
    toggleFavorite,
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
  beforeEach(() => {
    favorite = false
    toggleFavorite.mockClear()
  })

  it('renders compact metadata line', () => {
    render(
      <TrackList tracks={[sampleTrack]} albumId="a1" showMeta compact />,
    )
    expect(screen.getByText('Welcome to New York')).toBeTruthy()
    expect(screen.getByText('Pop · FLAC · 24-bit · 96 kHz')).toBeTruthy()
  })

  it('toggles favorites through the server-backed favorites hook', () => {
    render(
      <TrackList tracks={[sampleTrack]} albumId="a1" showFavorite compact />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Add to favorites' }))

    expect(toggleFavorite).toHaveBeenCalledWith(sampleTrack.id)
  })

  it('renders filled favorite state', () => {
    favorite = true

    render(
      <TrackList tracks={[sampleTrack]} albumId="a1" showFavorite compact />,
    )

    expect(
      screen.getByRole('button', { name: 'Remove from favorites' }),
    ).toBeTruthy()
  })
})

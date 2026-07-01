import { fireEvent, render, screen } from '@testing-library/react'
import type { AlbumDetail } from '@repo/api-client'
import { describe, expect, it, vi } from 'vitest'
import { AlbumDetailHeader } from './album-detail-header'

vi.mock('#/hooks/use-delete-library', () => ({
  useDeleteAlbum: () => ({ mutate: vi.fn(), isPending: false }),
  confirmDelete: () => false,
}))

vi.mock('#/lib/api', () => ({
  apiClient: {
    getAlbumCoverUrl: (albumId: string) => `/cover/${albumId}`,
  },
}))

const album = {
  id: 'album-1',
  title: 'Album 1',
  artistId: 'artist-1',
  artistName: 'Artist',
  year: 2024,
  trackCount: 2,
  tracks: [
    {
      id: 'track-1',
      title: 'Track 1',
      artistName: 'Artist',
      albumId: 'album-1',
      durationMs: 120000,
      format: 'flac',
    },
    {
      id: 'track-2',
      title: 'Track 2',
      artistName: 'Artist',
      albumId: 'album-1',
      durationMs: 180000,
      format: 'flac',
    },
  ],
} satisfies AlbumDetail

describe('AlbumDetailHeader', () => {
  it('queues the album without starting playback', () => {
    const onPlayAlbum = vi.fn()
    const onQueueAlbum = vi.fn()

    render(
      <AlbumDetailHeader
        album={album}
        onPlayAlbum={onPlayAlbum}
        onQueueAlbum={onQueueAlbum}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Queue album' }))

    expect(onQueueAlbum).toHaveBeenCalledOnce()
    expect(onPlayAlbum).not.toHaveBeenCalled()
  })
})

import { useMemo, useState, useDeferredValue } from 'react'
import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import {
  AlbumFilters,
  collectAlbumGenres,
  filterAlbums,
  type AlbumFilterState,
} from '#/components/album-filters'
import { AlbumGrid } from '#/components/album-grid'
import { ScanLibraryButton } from '#/components/scan-library-button'
import { apiClient } from '#/lib/api'

export const Route = createFileRoute('/library/albums/')({
  component: AlbumsPage,
})

const defaultFilters: AlbumFilterState = {
  albumQuery: '',
  artistId: 'all',
  genre: 'all',
}

function AlbumsPage() {
  const [filters, setFilters] = useState<AlbumFilterState>(defaultFilters)
  const deferredAlbumQuery = useDeferredValue(filters.albumQuery.trim())

  const artists = useQuery({
    queryKey: ['library', 'artists', 'all'],
    queryFn: () => apiClient.listArtists({ limit: 500 }),
    staleTime: 60_000,
  })

  const genreSource = useQuery({
    queryKey: ['library', 'albums', 'genre-source'],
    queryFn: () => apiClient.listAlbums({ limit: 500 }),
    staleTime: 60_000,
  })

  const albums = useQuery({
    queryKey: [
      'library',
      'albums',
      deferredAlbumQuery,
      filters.artistId,
    ],
    queryFn: () =>
      apiClient.listAlbums({
        limit: 500,
        q: deferredAlbumQuery || undefined,
        artistId:
          filters.artistId && filters.artistId !== 'all'
            ? filters.artistId
            : undefined,
      }),
  })

  const genreOptions = useMemo(
    () => collectAlbumGenres(genreSource.data?.items ?? []),
    [genreSource.data?.items],
  )

  const visibleAlbums = useMemo(
    () => filterAlbums(albums.data?.items ?? [], filters),
    [albums.data?.items, filters],
  )

  return (
    <div className="p-6">
        <header className="mb-6 flex flex-wrap items-start justify-between gap-4">
          <div>
            <h1 className="font-semibold text-2xl">Albums</h1>
            <p className="text-foreground text-sm">Browse albums in your library</p>
          </div>
          <ScanLibraryButton />
        </header>

        <AlbumFilters
          artists={artists.data?.items ?? []}
          genreOptions={genreOptions}
          filters={filters}
          onFiltersChange={setFilters}
          resultCount={visibleAlbums.length}
        />

        {albums.isLoading ? (
          <p className="text-foreground text-sm">Loading albums…</p>
        ) : albums.isError ? (
          <p className="text-destructive text-sm">Failed to load albums</p>
        ) : visibleAlbums.length === 0 ? (
          <p className="text-foreground text-sm">
            No albums match your filters.
          </p>
        ) : (
          <AlbumGrid albums={visibleAlbums} />
        )}
      </div>
  )
}

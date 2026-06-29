import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { AlbumGrid } from '#/components/album-grid'
import { MainHeader } from '#/components/main-header'
import { ScanBanner } from '#/components/scan-banner'
import { apiClient } from '#/lib/api'

export const Route = createFileRoute('/library/albums/')({
  component: AlbumsPage,
})

function AlbumsPage() {
  const [search, setSearch] = useState('')
  const albums = useQuery({
    queryKey: ['library', 'albums', search],
    queryFn: () => apiClient.listAlbums({ limit: 100, q: search || undefined }),
  })

  return (
    <>
      <MainHeader search={search} onSearchChange={setSearch} />
      <div className="p-6">
        <header className="mb-6">
          <h1 className="font-semibold text-2xl">Albums</h1>
          <p className="text-muted-foreground text-sm">Recent albums in your library</p>
        </header>
        <ScanBanner />
        {albums.isLoading ? (
          <p className="text-muted-foreground text-sm">Loading albums…</p>
        ) : albums.isError ? (
          <p className="text-destructive text-sm">Failed to load albums</p>
        ) : (
          <AlbumGrid albums={albums.data?.items ?? []} />
        )}
      </div>
    </>
  )
}

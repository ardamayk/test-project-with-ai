import { useCallback, useMemo } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '#/lib/api'

const playlistsKey = ['playlists'] as const
const playlistKey = (playlistId: string) => ['playlist', playlistId] as const

export function useFavoriteTracks() {
  const queryClient = useQueryClient()
  const playlists = useQuery({
    queryKey: playlistsKey,
    queryFn: () => apiClient.listPlaylists(),
  })

  const favoritesPlaylist = playlists.data?.items.find(
    (playlist) => playlist.isDefault && playlist.name === 'Favorites',
  )

  const favoritesDetail = useQuery({
    queryKey: favoritesPlaylist ? playlistKey(favoritesPlaylist.id) : ['playlist', 'favorites'],
    queryFn: () => apiClient.getPlaylist(favoritesPlaylist?.id ?? ''),
    enabled: Boolean(favoritesPlaylist),
  })

  const favorites = useMemo(
    () => favoritesDetail.data?.tracks.map((track) => track.id) ?? [],
    [favoritesDetail.data?.tracks],
  )

  const invalidateFavorites = async (playlistId: string) => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: playlistsKey }),
      queryClient.invalidateQueries({ queryKey: playlistKey(playlistId) }),
    ])
  }

  const addFavorite = useMutation({
    mutationFn: ({
      playlistId,
      trackId,
    }: {
      playlistId: string
      trackId: string
    }) => apiClient.addPlaylistTrack(playlistId, trackId),
    onSuccess: async (_data, vars) => {
      await invalidateFavorites(vars.playlistId)
    },
  })

  const removeFavorite = useMutation({
    mutationFn: ({
      playlistId,
      trackId,
    }: {
      playlistId: string
      trackId: string
    }) => apiClient.removePlaylistTrack(playlistId, trackId),
    onSuccess: async (_data, vars) => {
      await invalidateFavorites(vars.playlistId)
    },
  })

  const isFavorite = useCallback(
    (trackId: string) => favorites.includes(trackId),
    [favorites],
  )

  const toggleFavorite = useCallback(
    (trackId: string) => {
      if (!favoritesPlaylist) return
      const vars = { playlistId: favoritesPlaylist.id, trackId }
      if (favorites.includes(trackId)) {
        removeFavorite.mutate(vars)
      } else {
        addFavorite.mutate(vars)
      }
    },
    [addFavorite, favorites, favoritesPlaylist, removeFavorite],
  )

  return { favorites, isFavorite, toggleFavorite }
}

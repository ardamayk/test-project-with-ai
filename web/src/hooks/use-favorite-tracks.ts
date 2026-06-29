import { useCallback, useSyncExternalStore } from 'react'

const STORAGE_KEY = 'earthly-favorite-tracks'

let cachedRaw: string | null = null
let cachedSnapshot: readonly string[] = []

function parseFavorites(raw: string): readonly string[] {
  try {
    const parsed = JSON.parse(raw) as unknown
    return Array.isArray(parsed)
      ? parsed.filter((id): id is string => typeof id === 'string')
      : []
  } catch {
    return []
  }
}

function favoritesEqual(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((id, index) => id === b[index])
}

function getFavoritesSnapshot(): readonly string[] {
  const raw = localStorage.getItem(STORAGE_KEY) ?? ''
  if (raw === cachedRaw) {
    return cachedSnapshot
  }

  const next = parseFavorites(raw)
  if (favoritesEqual(next, cachedSnapshot)) {
    cachedRaw = raw
    return cachedSnapshot
  }

  cachedRaw = raw
  cachedSnapshot = next
  return cachedSnapshot
}

function writeFavorites(ids: string[]) {
  const raw = JSON.stringify(ids)
  localStorage.setItem(STORAGE_KEY, raw)
  cachedRaw = raw
  cachedSnapshot = ids
  window.dispatchEvent(new Event('earthly-favorites-changed'))
}

function subscribe(onChange: () => void) {
  const handler = () => onChange()
  window.addEventListener('earthly-favorites-changed', handler)
  window.addEventListener('storage', handler)
  return () => {
    window.removeEventListener('earthly-favorites-changed', handler)
    window.removeEventListener('storage', handler)
  }
}

export function useFavoriteTracks() {
  const favorites = useSyncExternalStore(
    subscribe,
    getFavoritesSnapshot,
    () => [],
  )

  const isFavorite = useCallback(
    (trackId: string) => favorites.includes(trackId),
    [favorites],
  )

  const toggleFavorite = useCallback((trackId: string) => {
    const current = [...getFavoritesSnapshot()]
    const next = current.includes(trackId)
      ? current.filter((id) => id !== trackId)
      : [...current, trackId]
    writeFavorites(next)
  }, [])

  return { favorites, isFavorite, toggleFavorite }
}

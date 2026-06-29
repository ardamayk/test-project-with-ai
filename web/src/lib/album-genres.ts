import type { AlbumDetail } from '@repo/api-client'

const genreDelimiterPattern = /[;/|,]+/

function splitGenreValue(value: string): string[] {
  const trimmed = value.trim()
  if (!trimmed) return []

  const seen = new Set<string>()
  const out: string[] = []
  for (const part of trimmed.split(genreDelimiterPattern)) {
    const genre = part.trim()
    if (!genre) continue
    const key = genre.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    out.push(genre)
  }
  return out
}

export function getAlbumGenres(album: AlbumDetail): string[] {
  if (album.genres && album.genres.length > 0) {
    return album.genres
  }

  const seen = new Set<string>()
  const out: string[] = []
  for (const track of album.tracks) {
    if (!track.genre) continue
    for (const genre of splitGenreValue(track.genre)) {
      const key = genre.toLowerCase()
      if (seen.has(key)) continue
      seen.add(key)
      out.push(genre)
    }
  }
  return out.sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' }))
}

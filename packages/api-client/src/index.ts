export type ApiClientConfig = {
  baseUrl: string
  getToken?: () => string | undefined
}

export type HealthResponse = {
  status: 'ok'
  version: string
}

export type ThemePreferences = {
  mode: 'light' | 'dark' | 'system'
  preset:
    | 'earthly'
    | 'tokyo-night'
    | 'vintage-harbor'
    | 'night-ember'
    | 'dusty-earth'
    | 'coastal-mist'
    | 'sage-hearth'
}

export type LayoutPreferences = {
  sidebarPosition: 'left' | 'right'
  panels: { left: string[]; right: string[] }
  collapsed: { left: boolean; right: boolean }
  sizes?: [number, number, number]
}

export type UserPreferences = {
  theme: ThemePreferences
  layout: LayoutPreferences
}

export type User = {
  id: string
  username: string
  displayName?: string
}

export type Artist = {
  id: string
  name: string
  albumCount?: number
}

export type ArtistList = {
  items: Artist[]
  total: number
}

export type Album = {
  id: string
  title: string
  artistId: string
  artistName: string
  year?: number
  trackCount?: number
  genres?: string[]
}

export type AlbumList = {
  items: Album[]
  total: number
}

export type Track = {
  id: string
  title: string
  artistName: string
  albumId: string
  albumTitle?: string
  trackNo?: number
  durationMs: number
  format: string
  genre?: string
  sizeBytes?: number
}

export type AlbumDetail = Album & {
  tracks: Track[]
}

export type TrackList = {
  items: Track[]
  total: number
}

export type ScanStatus = {
  status: 'idle' | 'running' | 'completed' | 'failed'
  scanned: number
  added: number
  updated: number
  removed: number
  error?: string
  startedAt?: string
  finishedAt?: string
}

export type DeleteResult = {
  deletedFiles: number
}

export type QueueItem = {
  id: string
  trackId: string
  position: number
  track: Track
}

export type Queue = {
  items: QueueItem[]
}

export type ErrorResponse = {
  error: string
  code: string
  message: string
}

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly body: ErrorResponse,
  ) {
    super(body.message)
    this.name = 'ApiError'
  }
}

export type ListParams = {
  limit?: number
  offset?: number
  q?: string
  artistId?: string
}

function buildQuery(params?: ListParams): string {
  if (!params) return ''
  const search = new URLSearchParams()
  if (params.limit != null) search.set('limit', String(params.limit))
  if (params.offset != null) search.set('offset', String(params.offset))
  if (params.q) search.set('q', params.q)
  if (params.artistId) search.set('artistId', params.artistId)
  const qs = search.toString()
  return qs ? `?${qs}` : ''
}

export function createApiClient(config: ApiClientConfig) {
  const { baseUrl, getToken } = config

  async function request<T>(
    path: string,
    init?: RequestInit,
  ): Promise<T> {
    const headers = new Headers(init?.headers)
    headers.set('Accept', 'application/json')
    if (init?.body) {
      headers.set('Content-Type', 'application/json')
    }
    const token = getToken?.()
    if (token) {
      headers.set('Authorization', `Bearer ${token}`)
    }

    const response = await fetch(`${baseUrl}${path}`, {
      ...init,
      headers,
    })

    if (!response.ok) {
      const body = (await response.json().catch(() => ({
        error: 'unknown',
        code: 'unknown',
        message: response.statusText,
      }))) as ErrorResponse
      throw new ApiError(response.status, body)
    }

    if (response.status === 204) {
      return undefined as T
    }

    return (await response.json()) as T
  }

  return {
    getHealth: () => request<HealthResponse>('/api/v1/health'),
    getMe: () => request<User>('/api/v1/me'),
    getPreferences: () => request<UserPreferences>('/api/v1/preferences'),
    patchPreferences: (body: Partial<UserPreferences>) =>
      request<UserPreferences>('/api/v1/preferences', {
        method: 'PATCH',
        body: JSON.stringify(body),
      }),

    triggerLibraryScan: () =>
      request<ScanStatus>('/api/v1/library/scan', { method: 'POST' }),
    getLibraryScanStatus: () =>
      request<ScanStatus>('/api/v1/library/scan/status'),
    listArtists: (params?: ListParams) =>
      request<ArtistList>(`/api/v1/library/artists${buildQuery(params)}`),
    listAlbums: (params?: ListParams) =>
      request<AlbumList>(`/api/v1/library/albums${buildQuery(params)}`),
    getAlbum: (albumId: string) =>
      request<AlbumDetail>(`/api/v1/library/albums/${albumId}`),
    deleteAlbum: (albumId: string) =>
      request<DeleteResult>(`/api/v1/library/albums/${albumId}`, {
        method: 'DELETE',
      }),
    listTracks: (params?: ListParams) =>
      request<TrackList>(`/api/v1/library/tracks${buildQuery(params)}`),
    getTrack: (trackId: string) =>
      request<Track>(`/api/v1/library/tracks/${trackId}`),
    deleteTrack: (trackId: string) =>
      request<DeleteResult>(`/api/v1/library/tracks/${trackId}`, {
        method: 'DELETE',
      }),

    getPlaybackQueue: () => request<Queue>('/api/v1/playback/queue'),
    replacePlaybackQueue: (trackIds: string[]) =>
      request<Queue>('/api/v1/playback/queue', {
        method: 'PUT',
        body: JSON.stringify({ trackIds }),
      }),
    appendPlaybackQueueItem: (trackId: string) =>
      request<Queue>('/api/v1/playback/queue/items', {
        method: 'POST',
        body: JSON.stringify({ trackId }),
      }),
    removePlaybackQueueItem: (itemId: string) =>
      request<Queue>(`/api/v1/playback/queue/items/${itemId}`, {
        method: 'DELETE',
      }),
    getTrackStreamUrl: (trackId: string) =>
      `${baseUrl}/api/v1/tracks/${trackId}/stream`,
    getAlbumCoverUrl: (albumId: string) =>
      `${baseUrl}/api/v1/library/albums/${albumId}/cover`,
  }
}

export type ApiClient = ReturnType<typeof createApiClient>

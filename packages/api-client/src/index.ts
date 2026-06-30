import type { components, operations } from './generated/schema'

export type ApiClientConfig = {
  baseUrl: string
  getToken?: () => string | undefined
}

type Schemas = components['schemas']

export type HealthResponse = Schemas['HealthResponse']
export type ThemePreferences = Schemas['ThemePreferences']
export type LayoutPreferences = Schemas['LayoutPreferences']
export type UserPreferences = Schemas['UserPreferences']
export type UserPreferencesPatch = Schemas['UserPreferencesPatch']
export type User = Schemas['User']
export type Artist = Schemas['Artist']
export type ArtistList = Schemas['ArtistList']
export type Album = Schemas['Album']
export type AlbumList = Schemas['AlbumList']
export type AlbumDetail = Schemas['AlbumDetail']
export type Track = Schemas['Track']
export type TrackList = Schemas['TrackList']
export type ScanStatus = Schemas['ScanStatus']
export type DeleteResult = Schemas['DeleteResult']
export type QueueItem = Schemas['QueueItem']
export type Queue = Schemas['Queue']
export type ErrorResponse = Schemas['ErrorResponse']
export type QueueReplace = Schemas['QueueReplace']
export type QueueItemAppend = Schemas['QueueItemAppend']

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly body: ErrorResponse,
  ) {
    super(body.message)
    this.name = 'ApiError'
  }
}

export type ListParams = NonNullable<
  operations['listAlbums']['parameters']['query']
>

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
    patchPreferences: (body: UserPreferencesPatch) =>
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

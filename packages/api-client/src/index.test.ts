import { describe, expect, expectTypeOf, it, vi } from 'vitest'
import type { operations } from './generated/schema'
import {
  type Album,
  type ArtistCredit,
  createApiClient,
  type Genre,
  type ReleaseIdentifier,
  type Track,
} from './index'

describe('normalized library contracts', () => {
  it('exposes ordered Artist credits, structured Genres, disc positions, editions, and artwork', () => {
    expectTypeOf<Album>().toHaveProperty('albumArtists')
    expectTypeOf<Album>().toHaveProperty('genreItems')
    expectTypeOf<Album>().toHaveProperty('releaseDate')
    expectTypeOf<Album>().toHaveProperty('releaseIdentifiers')
    expectTypeOf<Album>().toHaveProperty('artwork')
    expectTypeOf<Track>().toHaveProperty('artists')
    expectTypeOf<Track>().toHaveProperty('genres')
    expectTypeOf<Track>().toHaveProperty('discNo')
    expectTypeOf<Album['albumArtists']>().toEqualTypeOf<ArtistCredit[]>()
    expectTypeOf<Album['genreItems']>().toEqualTypeOf<Genre[]>()
    expectTypeOf<Album['releaseIdentifiers']>().toEqualTypeOf<ReleaseIdentifier[]>()
    expectTypeOf<Track['artists']>().toEqualTypeOf<ArtistCredit[]>()
    expectTypeOf<Track['genres']>().toEqualTypeOf<Genre[]>()
    expectTypeOf<Track['discNo']>().toEqualTypeOf<number>()
  })
})

describe('Track ReplayGain Metadata', () => {
  it('preserves missing values as null', () => {
    const replayGain: Track['replayGain'] = {
      trackGainDb: -7.25,
      trackPeak: null,
      albumGainDb: null,
      albumPeak: 1.01,
    }

    expect(replayGain).toEqual({
      trackGainDb: -7.25,
      trackPeak: null,
      albumGainDb: null,
      albumPeak: 1.01,
    })
  })
})

describe('createApiClient', () => {
  it('normalizes legacy library responses at the client boundary', async () => {
    const transport = vi.fn<typeof fetch>(async (input) => {
      if (input.toString().includes('/library/albums/')) {
        return Response.json({
          id: 'album-1',
          title: 'Legacy Album',
          artistId: 'artist-1',
          artistName: 'Legacy Artist',
          genres: ['Rock'],
          tracks: [
            {
              id: 'track-1',
              title: 'Legacy Track',
              artistName: 'Track Artist',
              albumId: 'album-1',
              durationMs: 1000,
              format: 'flac',
              genre: 'Electronic / Ambient',
            },
          ],
        })
      }
      return Response.json({
        items: [
          {
            id: 'track-1',
            title: 'Legacy Track',
            artistName: 'Track Artist',
            albumId: 'album-1',
            durationMs: 1000,
            format: 'flac',
            genre: 'Electronic / Ambient',
          },
        ],
        total: 1,
      })
    })
    const client = createApiClient({ baseUrl: '', transport })

    const album = await client.getAlbum('album-1')
    const tracks = await client.listTracks()

    expect(album.albumArtists).toEqual([{ id: 'artist-1', name: 'Legacy Artist' }])
    expect(album.genreItems).toEqual([{ id: 'legacy-genre:Rock', name: 'Rock' }])
    expect(album.releaseIdentifiers).toEqual([])
    expect(album.tracks[0]).toMatchObject({
      artists: [{ id: 'legacy-artist:Track Artist', name: 'Track Artist' }],
      discNo: 1,
      genres: [
        {
          id: 'legacy-genre:Electronic / Ambient',
          name: 'Electronic / Ambient',
        },
      ],
    })
    expect(tracks.items[0]?.discNo).toBe(1)
  })

  it('generates the missing revision response for Queue removal', () => {
    type RemoveQueueResponses = operations['removePlaybackQueueItem']['responses']

    expectTypeOf<RemoveQueueResponses>().toHaveProperty(400)
  })

  it('generates conflict responses for every Queue mutation', () => {
    type ReplaceResponses = operations['replacePlaybackQueue']['responses']
    type AppendResponses = operations['appendPlaybackQueueItem']['responses']
    type ReorderResponses = operations['reorderPlaybackQueue']['responses']
    type RemoveResponses = operations['removePlaybackQueueItem']['responses']

    expectTypeOf<ReplaceResponses>().toHaveProperty(409)
    expectTypeOf<AppendResponses>().toHaveProperty(409)
    expectTypeOf<ReorderResponses>().toHaveProperty(409)
    expectTypeOf<RemoveResponses>().toHaveProperty(409)
  })

  it('generates Queue event stream response', () => {
    type EventResponses = operations['streamPlaybackQueueEvents']['responses']

    expectTypeOf<EventResponses>().toHaveProperty(200)
  })

  it('subscribes to Queue invalidations and closes the event stream', async () => {
    const listeners = new Map<string, (event: MessageEvent<string>) => void>()
    const eventSource = {
      addEventListener: vi.fn((name: string, listener: (event: MessageEvent<string>) => void) => {
        listeners.set(name, listener)
      }),
      close: vi.fn(),
      onerror: null,
    }
    const eventSourceFactory = vi.fn(() => eventSource)
    const transport = vi.fn<typeof fetch>().mockResolvedValue(
      serverHealthResponse(['api.v1', 'playback.queue-events.v1']),
    )
    const client = createApiClient({
      baseUrl: '',
      queueEventsBaseUrl: async () => 'http://music.test',
      eventSourceFactory,
      transport,
    })
    const onEvent = vi.fn()

    const unsubscribe = client.subscribePlaybackQueueEvents(onEvent)
    await vi.waitFor(() => expect(eventSourceFactory).toHaveBeenCalled())
    listeners.get('queue-invalidated')?.({
      data: JSON.stringify({ revision: 'opaque-7', sequence: '7', invalidates: ['queue'] }),
    } as MessageEvent<string>)

    expect(eventSourceFactory).toHaveBeenCalledWith(
      'http://music.test/api/v1/playback/queue/events',
    )
    expect(onEvent).toHaveBeenCalledWith({
      revision: 'opaque-7',
      sequence: '7',
      invalidates: ['queue'],
    })
    unsubscribe()
    expect(eventSource.close).toHaveBeenCalledOnce()
  })

  it('recovers from a transient capability check failure without remounting', async () => {
    vi.useFakeTimers()
    try {
      const transport = vi.fn<typeof fetch>()
        .mockRejectedValueOnce(new Error('temporary health failure'))
        .mockRejectedValueOnce(new Error('temporary health failure'))
        .mockResolvedValueOnce(
          serverHealthResponse(['api.v1', 'playback.queue-events.v1']),
        )
      const eventSourceFactory = vi.fn(() => ({
        addEventListener: vi.fn(),
        close: vi.fn(),
        onerror: null,
      }))
      const client = createApiClient({
        baseUrl: 'http://music.test',
        transport,
        eventSourceFactory,
      })

      const unsubscribe = client.subscribePlaybackQueueEvents(vi.fn(), vi.fn())
      await Promise.resolve()
      await vi.advanceTimersByTimeAsync(999)
      expect(transport).toHaveBeenCalledOnce()
      await vi.advanceTimersByTimeAsync(1)
      expect(transport).toHaveBeenCalledTimes(2)
      await vi.advanceTimersByTimeAsync(1999)
      expect(transport).toHaveBeenCalledTimes(2)
      await vi.advanceTimersByTimeAsync(1)

      expect(transport).toHaveBeenCalledTimes(3)
      expect(eventSourceFactory).toHaveBeenCalledOnce()
      unsubscribe()
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not subscribe when Queue events capability is absent', async () => {
    vi.useFakeTimers()
    try {
      const transport = vi.fn<typeof fetch>().mockResolvedValue(
        serverHealthResponse(['api.v1']),
      )
      const eventSourceFactory = vi.fn()
      const onError = vi.fn()
      const client = createApiClient({
        baseUrl: 'http://music.test',
        transport,
        eventSourceFactory,
      })

      client.subscribePlaybackQueueEvents(vi.fn(), onError)
      await vi.advanceTimersByTimeAsync(0)
      await vi.advanceTimersByTimeAsync(120000)

      expect(onError).toHaveBeenCalledOnce()
      expect(onError).toHaveBeenCalledWith(
        new Error(
          'Music Server does not advertise playback.queue-events.v1; Queue synchronization is disabled.',
        ),
      )
      expect(transport).toHaveBeenCalledOnce()
      expect(eventSourceFactory).not.toHaveBeenCalled()
    } finally {
      vi.useRealTimers()
    }
  })

  it('cancels a pending capability retry when unsubscribed', async () => {
    vi.useFakeTimers()
    try {
      const transport = vi.fn<typeof fetch>().mockRejectedValue(
        new Error('temporary health failure'),
      )
      const eventSourceFactory = vi.fn()
      const client = createApiClient({
        baseUrl: 'http://music.test',
        transport,
        eventSourceFactory,
      })

      const unsubscribe = client.subscribePlaybackQueueEvents(vi.fn(), vi.fn())
      await vi.advanceTimersByTimeAsync(0)
      unsubscribe()
      await vi.advanceTimersByTimeAsync(120000)

      expect(transport).toHaveBeenCalledOnce()
      expect(eventSourceFactory).not.toHaveBeenCalled()
    } finally {
      vi.useRealTimers()
    }
  })

  it('authenticates Queue event streams when a token is configured', async () => {
    const transport = vi.fn<typeof fetch>().mockImplementation(async (input) => {
      if (input.toString().endsWith('/api/v1/health')) {
        return serverHealthResponse(['api.v1', 'playback.queue-events.v1'])
      }
      return new Response(
        'id: 8\nevent: queue-invalidated\ndata: {"revision":"opaque-8","sequence":"8","invalidates":["queue"]}\n\n',
        { status: 200, headers: { 'Content-Type': 'text/event-stream' } },
      )
    })
    const client = createApiClient({
      baseUrl: 'http://music.test',
      getToken: () => 'secret-token',
      transport,
    })
    const onEvent = vi.fn()

    const unsubscribe = client.subscribePlaybackQueueEvents(onEvent)
    await vi.waitFor(() => expect(onEvent).toHaveBeenCalled())
    unsubscribe()

    const eventRequest = transport.mock.calls.find(([input]) =>
      input.toString().endsWith('/api/v1/playback/queue/events'),
    )
    const headers = new Headers(eventRequest?.[1]?.headers)
    expect(headers.get('Authorization')).toBe('Bearer secret-token')
    expect(onEvent).toHaveBeenCalledWith({
      revision: 'opaque-8',
      sequence: '8',
      invalidates: ['queue'],
    })
  })
  it('builds health URL from base', () => {
    const client = createApiClient({ baseUrl: 'http://localhost:8080' })
    expect(client.getHealth).toBeTypeOf('function')
    expect(client.getPreferences).toBeTypeOf('function')
    expect(client.listAlbums).toBeTypeOf('function')
    expect(client.getPlaybackQueue).toBeTypeOf('function')
    expect(client.listPlaylists).toBeTypeOf('function')
    expect(client.createPlaylist).toBeTypeOf('function')
    expect(client.addPlaylistTrack).toBeTypeOf('function')
    expect(client.removePlaylistTrack).toBeTypeOf('function')
    expect(client.deleteAlbum).toBeTypeOf('function')
    expect(client.deleteTrack).toBeTypeOf('function')
    expect(client.listRadioStations).toBeTypeOf('function')
    expect(client.getRadioStation).toBeTypeOf('function')
    expect(client.searchRadioStations).toBeTypeOf('function')
    expect(client.getTrackStreamUrl('abc')).toBe(
      'http://localhost:8080/api/v1/tracks/abc/stream',
    )
    expect(client.getRadioStationStreamUrl('station-1')).toBe(
      'http://localhost:8080/api/v1/radio/stations/station-1/stream',
    )
  })

  it('uses an injected transport for desktop requests', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(
        JSON.stringify({
          status: 'ok',
          version: '0.1.0',
          capabilities: ['api.v1'],
        }),
        { status: 200 },
      ),
    )
    const client = createApiClient({
      baseUrl: 'http://127.0.0.1:8090',
      transport,
    })

    await expect(client.getHealth()).resolves.toMatchObject({ status: 'ok' })
    expect(transport).toHaveBeenCalledWith(
      'http://127.0.0.1:8090/api/v1/health',
      expect.objectContaining({ headers: expect.any(Headers) }),
    )
  })

  it('builds media URLs from a separate native protocol base', () => {
    let mediaBaseUrl = 'http://127.0.0.1:41000/token-1'
    const client = createApiClient({
      baseUrl: '',
      mediaBaseUrl: 'earthly-media://localhost',
      streamBaseUrl: () => mediaBaseUrl,
    })

    expect(client.getAlbumCoverUrl('album-1')).toBe(
      'earthly-media://localhost/api/v1/library/albums/album-1/cover',
    )
    expect(client.getTrackStreamUrl('track-1')).toBe(
      'http://127.0.0.1:41000/token-1/api/v1/tracks/track-1/stream',
    )
    expect(client.getRadioStationStreamUrl('station-1')).toBe(
      'http://127.0.0.1:41000/token-1/api/v1/radio/stations/station-1/stream',
    )
    expect(client.getRadioCatalogPreviewStreamUrl('preview-1')).toBe(
      'http://127.0.0.1:41000/token-1/api/v1/radio/preview/preview-1/stream',
    )

    mediaBaseUrl = 'http://127.0.0.1:42000/token-2'
    expect(client.getTrackStreamUrl('track-2')).toBe(
      'http://127.0.0.1:42000/token-2/api/v1/tracks/track-2/stream',
    )
  })

  it('sends Queue Revisions with every mutation', async () => {
    const transport = vi.fn<typeof fetch>().mockImplementation(async () =>
      new Response(JSON.stringify({ items: [], revision: '2' }), { status: 200 }),
    )
    const client = createApiClient({ baseUrl: '', transport })

    await client.replacePlaybackQueue(['track-1'], '1')
    await client.appendPlaybackQueueItem('track-1', '1')
    await client.removePlaybackQueueItem('item-1', '1')
    await client.reorderPlaybackQueue(['item-1'], '1')

    expect(JSON.parse(String(transport.mock.calls[0]?.[1]?.body))).toEqual({
      trackIds: ['track-1'],
      revision: '1',
    })
    expect(JSON.parse(String(transport.mock.calls[1]?.[1]?.body))).toEqual({
      trackId: 'track-1',
      revision: '1',
    })
    expect(new Headers(transport.mock.calls[2]?.[1]?.headers).get('If-Match')).toBe('1')
    expect(JSON.parse(String(transport.mock.calls[3]?.[1]?.body))).toEqual({
      itemIds: ['item-1'],
      revision: '1',
    })
  })

  it('decodes conflict responses for every Queue mutation', async () => {
    const conflict = {
      error: 'conflict',
      code: 'queue_revision_conflict',
      message: 'queue changed since supplied revision',
      queue: { items: [], revision: '2' },
    }
    const transport = vi.fn<typeof fetch>().mockImplementation(async () =>
      new Response(JSON.stringify(conflict), { status: 409 }),
    )
    const client = createApiClient({ baseUrl: '', transport })
    const mutations = [
      () => client.replacePlaybackQueue(['track-1'], '1'),
      () => client.appendPlaybackQueueItem('track-1', '1'),
      () => client.reorderPlaybackQueue([], '1'),
      () => client.removePlaybackQueueItem('item-1', '1'),
    ]

    for (const mutation of mutations) {
      await expect(mutation()).rejects.toMatchObject({
        status: 409,
        body: conflict,
      })
    }
  })
})

function serverHealthResponse(capabilities: string[]) {
  return new Response(
    JSON.stringify({ status: 'ok', version: '0.1.0', capabilities }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  )
}

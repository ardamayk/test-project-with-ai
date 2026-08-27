import { describe, expect, expectTypeOf, it, vi } from 'vitest'
import type { operations } from './generated/schema'
import { createApiClient, type Track } from './index'

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

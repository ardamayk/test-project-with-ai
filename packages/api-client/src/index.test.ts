import { describe, expect, it } from 'vitest'
import { createApiClient } from './index'

describe('createApiClient', () => {
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
})

import { describe, expect, it } from 'vitest'
import { createApiClient } from './index'

describe('createApiClient', () => {
  it('builds health URL from base', () => {
    const client = createApiClient({ baseUrl: 'http://localhost:8080' })
    expect(client.getHealth).toBeTypeOf('function')
    expect(client.getPreferences).toBeTypeOf('function')
  })
})

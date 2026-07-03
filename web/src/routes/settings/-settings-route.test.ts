import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('settings route', () => {
  it('persists preferences only through LayoutProvider onPreferencesChange', () => {
    const source = readFileSync('src/routes/settings/index.tsx', 'utf8')

    expect(source).not.toContain('patchPreferences.mutate')
  })
})

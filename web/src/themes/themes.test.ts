import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import {
  contrastRatio,
  isDarkToLightOrder,
  parsePaletteFromCss,
  type HexColor,
} from './palette-utils'
import { THEME_PRESET_REGISTRY, THEME_PRESET_IDS } from './registry'
import { themeSlotContract } from './tokens'

const themesDir = join(import.meta.dirname)
const layersCss = readFileSync(join(themesDir, 'layers.css'), 'utf8')
const openapiYaml = readFileSync(
  join(import.meta.dirname, '../../../packages/contracts/openapi.yaml'),
  'utf8',
)

function paletteCssPath(id: string): string {
  return join(themesDir, `${id}.css`)
}

function semanticCssPath(id: string): string {
  return join(themesDir, 'semantic', `${id}.css`)
}

function readPaletteCss(id: string): string {
  return readFileSync(paletteCssPath(id), 'utf8')
}

function slotColor(
  colors: readonly HexColor[],
  slot: `theme-${1 | 2 | 3 | 4 | 5 | 6}`,
): HexColor {
  return colors[Number(slot.replace('theme-', '')) - 1]
}

describe('theme registry', () => {
  it('matches OpenAPI preset enum', () => {
    const enumBlock = openapiYaml.match(/preset:\s*\n\s*type: string\s*\n\s*enum:\s*([\s\S]*?)(?:\n\s{4}\w|\n    [A-Z])/m)
    expect(enumBlock).not.toBeNull()
    const openapiIds = [...enumBlock![1].matchAll(/- ([a-z0-9-]+)/g)].map(
      (match) => match[1],
    )
    expect(THEME_PRESET_IDS).toEqual(openapiIds)
  })

  it('has a palette CSS file per preset', () => {
    for (const preset of THEME_PRESET_REGISTRY) {
      expect(() => readPaletteCss(preset.id)).not.toThrow()
    }
  })

  it('keeps registry colors in sync with palette CSS files', () => {
    for (const preset of THEME_PRESET_REGISTRY) {
      const parsed = parsePaletteFromCss(readPaletteCss(preset.id))
      expect(parsed).toEqual(preset.colors.map((c) => c.toLowerCase()))
    }
  })

  it('orders palette colors dark → light', () => {
    for (const preset of THEME_PRESET_REGISTRY) {
      expect(isDarkToLightOrder(preset.colors as HexColor[])).toBe(true)
    }
  })

  it('declares semantic overrides only when the file exists', () => {
    for (const preset of THEME_PRESET_REGISTRY) {
      if (preset.semanticOverride) {
        expect(() => readFileSync(semanticCssPath(preset.id), 'utf8')).not.toThrow()
      }
    }
  })
})

describe('theme slot contract', () => {
  it('layers.css maps readable text to palette poles, not chrome slots', () => {
    expect(layersCss).toContain('--heading: var(--theme-6)')
    expect(layersCss).toContain('--text: var(--theme-5)')
    expect(layersCss).not.toMatch(/--foreground:\s*var\(--theme-[34]\)/)
    expect(layersCss).not.toMatch(/--sidebar-foreground:\s*var\(--theme-[34]\)/)
  })

  it('layers.css maps shell surfaces per dark/light contract', () => {
    expect(layersCss).toContain('--sidebar: var(--theme-2)')
    expect(layersCss).toContain('--queue: var(--theme-2)')
    expect(layersCss).toContain('--player: var(--theme-1)')
    expect(layersCss).toContain('--sidebar: var(--theme-5)')
    expect(layersCss).toContain('--queue: var(--theme-5)')
    expect(layersCss).toContain('--player: var(--theme-6)')
  })

  it('meets minimum contrast for default dark/light text on shell surfaces', () => {
    for (const preset of THEME_PRESET_REGISTRY) {
      const colors = preset.colors as HexColor[]
      const dark = themeSlotContract.dark
      const light = themeSlotContract.light

      expect(
        contrastRatio(
          slotColor(colors, dark.heading),
          slotColor(colors, dark.shellSidebar),
        ),
      ).toBeGreaterThanOrEqual(4.5)
      expect(
        contrastRatio(
          slotColor(colors, dark.bodyText),
          slotColor(colors, dark.shellPlayer),
        ),
      ).toBeGreaterThanOrEqual(4.5)
      expect(
        contrastRatio(
          slotColor(colors, light.heading),
          slotColor(colors, light.shellSidebar),
        ),
      ).toBeGreaterThanOrEqual(4.5)
      expect(
        contrastRatio(
          slotColor(colors, light.bodyText),
          slotColor(colors, light.mainBackground),
        ),
      ).toBeGreaterThanOrEqual(4.5)
    }
  })
})

describe('themePresetOptions', () => {
  it('exposes six swatches per preset', async () => {
    const { themePresetOptions } = await import('./presets')
    for (const option of themePresetOptions) {
      expect(option.swatches).toHaveLength(6)
    }
  })
})

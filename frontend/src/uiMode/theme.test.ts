import { beforeEach, describe, expect, it, vi } from 'vitest'

import { DEFAULT_THEME, readThemePreference, writeThemePreference } from './theme'

describe('theme preference', () => {
  beforeEach(() => {
    const values = new Map<string, string>()
    vi.stubGlobal('localStorage', {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => values.set(key, value),
    })
  })

  it('uses one default and persists supported themes', () => {
    expect(readThemePreference()).toBe(DEFAULT_THEME)
    expect(writeThemePreference('system')).toBe('system')
    expect(readThemePreference()).toBe('system')
  })

  it('normalizes unsupported values', () => {
    expect(writeThemePreference('missing')).toBe(DEFAULT_THEME)
    expect(readThemePreference()).toBe(DEFAULT_THEME)
  })
})

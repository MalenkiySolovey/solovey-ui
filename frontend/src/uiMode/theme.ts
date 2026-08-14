export const DEFAULT_THEME = 'dark'

export type ThemePreference = 'light' | 'dark' | 'system'

export const isThemePreference = (value: unknown): value is ThemePreference =>
  value === 'light' || value === 'dark' || value === 'system'

export const readThemePreference = (): ThemePreference => {
  try {
    const stored = localStorage.getItem('theme')
    return isThemePreference(stored) ? stored : DEFAULT_THEME
  } catch {
    return DEFAULT_THEME
  }
}

export const writeThemePreference = (value: string): ThemePreference => {
  const normalized = isThemePreference(value) ? value : DEFAULT_THEME
  try {
    localStorage.setItem('theme', normalized)
  } catch {
    // Keep the active in-memory Vuetify theme when storage is unavailable.
  }
  return normalized
}
